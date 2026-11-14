import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { CopyOriginNotice } from "./CopyOriginNotice";
import { ShareCoursePanel } from "./ShareCourse";
import { UnpublishDialog } from "./UnpublishDialog";
import { originMessage, unpublish } from "./retention";
import { session } from "../accounts/session";

vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));
const request = vi.mocked(session.request);
const status = (status: number) =>
  Object.assign(new Error("server"), { status });

beforeEach(() => {
  request.mockReset();
});

describe("copy origin wording", () => {
  it("names the creator while the account exists", () => {
    expect(
      originMessage({
        creator_username: "ada",
        creator_deleted: false,
        source_available: true,
      }),
    ).toEqual({ title: "Created by @ada", detail: "" });
  });

  it("anonymizes a deleted creator and says the copy keeps working", () => {
    const m = originMessage({
      creator_username: "ada",
      creator_deleted: true,
      source_available: false,
    });
    expect(m?.title).toBe("Created by deleted user");
    expect(m?.detail).toContain("This copy keeps working");
    expect(JSON.stringify(m)).not.toContain("ada");
  });

  it("explains an unshared source without blaming the learner", () => {
    const m = originMessage({
      creator_username: "ada",
      creator_deleted: false,
      source_available: false,
    });
    expect(m?.title).toBe("Created by @ada");
    expect(m?.detail).toContain("no longer shared");
    expect(m?.detail).toContain("This copy keeps working");
  });

  it("shows nothing for a course the learner made themselves", () => {
    expect(originMessage(undefined)).toBeNull();
    expect(originMessage(null)).toBeNull();
  });

  it("renders as a note and never as an alert", () => {
    render(
      <CopyOriginNotice
        origin={{ creator_deleted: true, source_available: false }}
      />,
    );
    const note = screen.getByRole("note");
    expect(note).toHaveTextContent("Created by deleted user");
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("renders nothing without an origin", () => {
    const { container } = render(<CopyOriginNotice origin={null} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe("unpublish request", () => {
  it("posts to the course route and sends no identity", async () => {
    request.mockResolvedValueOnce(undefined);
    await unpublish("c1");
    expect(request).toHaveBeenCalledWith(
      "/v1/courses/c1/unpublish",
      "POST",
      {},
    );
  });
});

describe("unpublish dialog", () => {
  const open = (onDone = vi.fn(), onClose = vi.fn()) => {
    render(<UnpublishDialog courseId="c1" onDone={onDone} onClose={onClose} />);
    return { onDone, onClose };
  };

  it("states the consequences and starts on the safe choice", () => {
    open();
    const dialog = screen.getByRole("dialog", {
      name: "Unpublish this course?",
    });
    expect(dialog).toHaveTextContent("keep their copy");
    expect(dialog).toHaveTextContent("link");
    expect(
      screen.getByRole("button", { name: "Keep published" }),
    ).toHaveFocus();
  });

  it("closes on Escape and on Keep published without a request", () => {
    const { onClose } = open();
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    fireEvent.click(screen.getByRole("button", { name: "Keep published" }));
    expect(onClose).toHaveBeenCalledTimes(2);
    expect(request).not.toHaveBeenCalled();
  });

  it("unpublishes once, disables the button while pending and reports done", async () => {
    let resolve: () => void = () => {};
    request.mockReturnValueOnce(new Promise<void>((r) => (resolve = r)));
    const { onDone } = open();
    const confirm = screen.getByRole("button", { name: "Unpublish" });
    fireEvent.click(confirm);
    fireEvent.click(confirm);
    expect(confirm).toBeDisabled();
    resolve();
    await waitFor(() => expect(onDone).toHaveBeenCalledTimes(1));
    expect(request).toHaveBeenCalledTimes(1);
  });

  it("offers a retry after an offline failure", async () => {
    request
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
      .mockResolvedValueOnce(undefined);
    const { onDone } = open();
    fireEvent.click(screen.getByRole("button", { name: "Unpublish" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("offline");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(onDone).toHaveBeenCalled());
  });

  it("shows a neutral message for a refusal and never the server text", async () => {
    request.mockRejectedValueOnce(status(404));
    open();
    fireEvent.click(screen.getByRole("button", { name: "Unpublish" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("isn't available");
    expect(alert).not.toHaveTextContent("server");
  });
});

describe("share panel unpublish entry", () => {
  const panel = () =>
    render(<ShareCoursePanel courseId="c1" onClose={() => {}} />);

  it("offers Unpublish course only while the course is shared", async () => {
    request.mockResolvedValueOnce({ visibility: "private", active: false });
    panel();
    await screen.findByRole("radio", { name: "Private" });
    expect(
      screen.queryByRole("button", { name: "Unpublish course" }),
    ).toBeNull();
  });

  it("unpublishes from the panel and shows the course as private", async () => {
    request
      .mockResolvedValueOnce({
        visibility: "link",
        active: true,
        version: 1,
        lesson_count: 3,
      })
      .mockResolvedValueOnce(undefined);
    panel();
    fireEvent.click(
      await screen.findByRole("button", { name: "Unpublish course" }),
    );
    fireEvent.click(
      within(
        screen.getByRole("dialog", { name: "Unpublish this course?" }),
      ).getByRole("button", { name: "Unpublish" }),
    );
    expect(await screen.findByText("Unpublished")).toBeVisible();
    expect(screen.getByRole("radio", { name: "Private" })).toBeChecked();
    expect(request).toHaveBeenCalledWith(
      "/v1/courses/c1/unpublish",
      "POST",
      {},
    );
  });
});
