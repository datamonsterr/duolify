import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { EnrollPanel } from "./EnrollPanel";
import { enrollFailure, enrollMessages } from "./links";
import { session } from "../accounts/session";

vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));
const request = vi.mocked(session.request);
const TOKEN = "Hq7aBcDeFgHiJkLmNoPqRsTuVwX";
const result = (created: boolean) => ({
  course_id: "copy-1",
  created,
  version: 3,
  total_lessons: 3,
  ready_lessons: 2,
  needs_generation_lessons: 1,
});
const rejected = (status: number) =>
  Object.assign(new Error("server text"), { status });
const panel = (signedIn = true) =>
  render(<EnrollPanel token={TOKEN} signedIn={signedIn} ready={2} total={3} />);

beforeEach(() => request.mockReset());

describe("enroll failures", () => {
  it("classifies by status only and never shows server text", () => {
    expect(enrollFailure(rejected(404))).toBe("unavailable");
    expect(enrollFailure(rejected(429))).toBe("rate_limited");
    expect(enrollFailure(rejected(500))).toBe("offline");
    expect(enrollFailure(new TypeError("Failed to fetch"))).toBe("offline");
    for (const text of Object.values(enrollMessages))
      expect(text).not.toContain("server text");
  });
});

describe("enroll panel", () => {
  it("asks a signed-out visitor to sign in and offers no enroll button", () => {
    panel(false);
    expect(
      screen.getByRole("link", { name: "Sign in to enroll" }),
    ).toHaveAttribute(
      "href",
      `/sign-in?returnTo=${encodeURIComponent(`/c/${TOKEN}`)}`,
    );
    expect(screen.queryByRole("button", { name: "Enroll" })).toBeNull();
    expect(request).not.toHaveBeenCalled();
  });

  it("enrolls once on click and links to the pinned copy", async () => {
    request.mockResolvedValueOnce({ enrolled: false });
    panel();
    const button = await screen.findByRole("button", { name: "Enroll" });
    request.mockResolvedValueOnce(result(true));
    fireEvent.click(button);
    expect(await screen.findByTestId("enroll-status")).toHaveTextContent(
      "Enrolled",
    );
    expect(screen.getByTestId("enroll-status")).not.toHaveTextContent(
      "Already",
    );
    expect(screen.getByRole("link", { name: "Open course" })).toHaveAttribute(
      "href",
      "/course/copy-1",
    );
    expect(request).toHaveBeenCalledWith(
      `/v1/public/shares/${TOKEN}/enroll`,
      "POST",
      {},
    );
    expect(screen.getByText(/version 3/i)).toBeInTheDocument();
  });

  it("shows an existing copy without asking again", async () => {
    request.mockResolvedValueOnce({ enrolled: true, course_id: "copy-9" });
    panel();
    expect(await screen.findByTestId("enroll-status")).toHaveTextContent(
      "Already enrolled",
    );
    expect(screen.getByRole("link", { name: "Open course" })).toHaveAttribute(
      "href",
      "/course/copy-9",
    );
    expect(screen.queryByRole("button", { name: "Enroll" })).toBeNull();
    expect(request).toHaveBeenCalledTimes(1);
  });

  it("treats created=false as already enrolled", async () => {
    request.mockResolvedValueOnce({ enrolled: false });
    panel();
    request.mockResolvedValueOnce(result(false));
    fireEvent.click(await screen.findByRole("button", { name: "Enroll" }));
    expect(await screen.findByTestId("enroll-status")).toHaveTextContent(
      "Already enrolled",
    );
  });

  it("sends one request for a double activation", async () => {
    request.mockResolvedValueOnce({ enrolled: false });
    panel();
    const button = await screen.findByRole("button", { name: "Enroll" });
    let finish: (v: unknown) => void = () => {};
    request.mockReturnValueOnce(new Promise((resolve) => (finish = resolve)));
    fireEvent.click(button);
    fireEvent.click(button);
    expect(button).toBeDisabled();
    finish(result(true));
    await screen.findByTestId("enroll-status");
    expect(request).toHaveBeenCalledTimes(2); // status read + one enroll
  });

  it("shows the neutral refusal for a dead link with no retry", async () => {
    request.mockResolvedValueOnce({ enrolled: false });
    panel();
    request.mockRejectedValueOnce(rejected(404));
    fireEvent.click(await screen.findByRole("button", { name: "Enroll" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /no longer active/i,
    );
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Enroll" })).toBeNull();
  });

  it("recovers from a rate limit and an offline error with Retry", async () => {
    request.mockResolvedValueOnce({ enrolled: false });
    panel();
    request.mockRejectedValueOnce(rejected(429));
    fireEvent.click(await screen.findByRole("button", { name: "Enroll" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/Too many/);
    request.mockRejectedValueOnce(new TypeError("Failed to fetch"));
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Couldn't reach Duolify/,
    );
    request.mockResolvedValueOnce(result(true));
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() =>
      expect(screen.getByTestId("enroll-status")).toHaveTextContent("Enrolled"),
    );
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("says ready lessons are free and the rest cost credits", async () => {
    request.mockResolvedValueOnce({ enrolled: false });
    panel();
    expect(await screen.findByText(/2 of 3 lessons ready/)).toBeInTheDocument();
    expect(screen.getByText(/Ready lessons are free/)).toBeInTheDocument();
  });
});
