import { beforeEach as keepOldForms } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { BirthDatePrompt, publisherAgeError } from "./BirthDatePrompt";
import { ShareCoursePanel } from "./ShareCourse";
import { session, AuthError } from "../accounts/session";
import { drafts } from "./drafts";

vi.mock("../accounts/session", async (original) => ({
  ...(await original<object>()),
  session: { request: vi.fn() },
}));
const request = vi.mocked(session.request);
beforeEach(() => {
  request.mockReset();
  vi.restoreAllMocks();
});

it("recognizes only typed publisher age errors", () => {
  expect(
    publisherAgeError(
      new AuthError("arbitrary", 409, { error: "birth_date_required" }),
    ),
  ).toBe("birth_date_required");
  expect(
    publisherAgeError(
      new AuthError("arbitrary", 403, { error: "adult_required" }),
    ),
  ).toBe("adult_required");
  expect(
    publisherAgeError(
      new AuthError("birth_date_required", 409, { error: "other" }),
    ),
  ).toBeNull();
});

it("labels and focuses the date, announces a server error, retries and resumes only after save", async () => {
  const onSaved = vi.fn();
  request
    .mockRejectedValueOnce(
      new AuthError("private date", 400, { error: "invalid_birth_date" }),
    )
    .mockResolvedValueOnce(undefined);
  render(<BirthDatePrompt onSaved={onSaved} onCancel={() => {}} />);
  const input = screen.getByLabelText("Birth date", { exact: true });
  expect(input).toHaveFocus();
  fireEvent.change(input, { target: { value: "2999-01-01" } });
  fireEvent.submit(
    screen.getByRole("form", { name: "Birth date for publishing" }),
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Enter a real birth date",
  );
  expect(input).toHaveAttribute("aria-invalid", "true");
  expect(onSaved).not.toHaveBeenCalled();
  fireEvent.change(input, { target: { value: "1990-06-15" } });
  fireEvent.submit(
    screen.getByRole("form", { name: "Birth date for publishing" }),
  );
  await waitFor(() => expect(onSaved).toHaveBeenCalledOnce());
  expect(request).toHaveBeenLastCalledWith("/v1/me/birth-date", "PUT", {
    birth_date: "1990-06-15",
  });
  expect(screen.queryByText("private date")).toBeNull();
});

it("asks for a missing date and resumes the existing screened publication flow", async () => {
  vi.spyOn(drafts, "disclosure").mockResolvedValue({
    names: [],
    character_count: 0,
    custom_count: 0,
    exports_enabled: false,
  });
  request
    .mockResolvedValueOnce({ visibility: "private", active: false })
    .mockResolvedValueOnce({
      id: "d",
      number: 1,
      verdict: "pass",
      lesson_count: 1,
    })
    .mockRejectedValueOnce(
      new AuthError("", 409, { error: "birth_date_required" }),
    )
    .mockResolvedValueOnce(undefined)
    .mockResolvedValueOnce({
      id: "d2",
      number: 2,
      verdict: "pass",
      lesson_count: 1,
    })
    .mockResolvedValueOnce({
      preview_url: "https://share.example.test/c/abcdefghabcdefghabcdefgh",
      token: "t",
      deep_link: "duolify://share/t",
    });
  render(<ShareCoursePanel courseId="c1" onClose={() => {}} />);
  fireEvent.click(
    await screen.findByRole("radio", { name: /Anyone with the link/ }),
  );
  fireEvent.change(
    await screen.findByLabelText("Birth date", { exact: true }),
    { target: { value: "1990-06-15" } },
  );
  fireEvent.click(screen.getByRole("button", { name: "Save and continue" }));
  expect(await screen.findByTestId("share-link-url")).toBeVisible();
  expect(screen.queryByLabelText("Birth date", { exact: true })).toBeNull();
});

it("replaces a locked birth-date form with a support message and no resubmission path", async () => {
  const onSaved = vi.fn();
  const onCancel = vi.fn();
  request.mockRejectedValueOnce(
    new AuthError("private server response", 409, {
      error: "birth_date_locked",
    }),
  );
  render(<BirthDatePrompt onSaved={onSaved} onCancel={onCancel} />);
  fireEvent.change(screen.getByLabelText("Birth date", { exact: true }), {
    target: { value: "1990-06-15" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save and continue" }));
  const notice = await screen.findByRole("alert");
  expect(notice).toHaveTextContent(
    "A birth date is already on your account. Contact support if it needs correcting.",
  );
  await waitFor(() => expect(notice).toHaveFocus());
  expect(screen.queryByLabelText("Birth date", { exact: true })).toBeNull();
  expect(screen.queryByRole("form")).toBeNull();
  expect(
    screen.queryByRole("button", { name: "Save and continue" }),
  ).toBeNull();
  expect(screen.queryByText("private server response")).toBeNull();
  expect(screen.queryByText("1990-06-15")).toBeNull();
  expect(onSaved).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Back to sharing" }));
  expect(onCancel).toHaveBeenCalledOnce();
  expect(request).toHaveBeenCalledOnce();
});

it("shows an under-18 refusal without treating it as a connection failure", async () => {
  vi.spyOn(drafts, "disclosure").mockResolvedValue({
    names: [],
    character_count: 0,
    custom_count: 0,
    exports_enabled: false,
  });
  request
    .mockResolvedValueOnce({ visibility: "private", active: false })
    .mockResolvedValueOnce({
      id: "d",
      number: 1,
      verdict: "pass",
      lesson_count: 1,
    })
    .mockRejectedValueOnce(new AuthError("", 403, { error: "adult_required" }));
  render(<ShareCoursePanel courseId="c1" onClose={() => {}} />);
  fireEvent.click(
    await screen.findByRole("radio", { name: /Anyone with the link/ }),
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "You must be 18 or older to publish",
  );
  expect(screen.queryByTestId("share-link-url")).toBeNull();
  expect(screen.queryByLabelText("Birth date", { exact: true })).toBeNull();
  expect(
    screen.queryByRole("button", { name: "Save and continue" }),
  ).toBeNull();
});

// The old forms stay reachable behind `duolify.guided.rest` until 8.3 removes them.
keepOldForms(() => localStorage.setItem("duolify.guided.rest", "off"));
