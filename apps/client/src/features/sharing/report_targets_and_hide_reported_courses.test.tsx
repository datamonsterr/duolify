import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { CourseReviews } from "./CourseReviews";
import { MarketCoursePage } from "./MarketCoursePage";
import { ReportSheet } from "./ReportSheet";
import { reportReasons, reportFailure } from "./reports";
import { session } from "../accounts/session";

vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));
const request = vi.mocked(session.request);
const rejected = (status: number) =>
  Object.assign(new Error("server text: pq password"), { status });
beforeEach(() => request.mockReset());

const open = (onClose = vi.fn()) => {
  render(
    <ReportSheet
      target={{ type: "course", id: "course-1" }}
      title="Report this course"
      onClose={onClose}
    />,
  );
  return onClose;
};

describe("report sheet", () => {
  it("offers the configured reasons, focuses the first and needs a choice", () => {
    open();
    const dialog = screen.getByRole("dialog", { name: "Report this course" });
    expect(
      Array.from(dialog.querySelectorAll("label.report-reason")).map(
        (l) => l.textContent,
      ),
    ).toEqual([
      "Unsafe instructions",
      "Inappropriate content",
      "Spam",
      "Copyright",
      "Misleading",
      "Other",
    ]);
    expect(reportReasons.map((r) => r.id)).toEqual([
      "unsafe",
      "inappropriate",
      "spam",
      "copyright",
      "misleading",
      "other",
    ]);
    expect(screen.getByLabelText("Unsafe instructions")).toHaveFocus();
    expect(screen.getByRole("button", { name: "Send report" })).toBeDisabled();
  });

  it("sends reason and details, then shows the thanks state", async () => {
    open();
    request.mockResolvedValueOnce({ status: "received", merged: false });
    fireEvent.click(screen.getByLabelText("Copyright"));
    fireEvent.change(screen.getByLabelText("Details, optional"), {
      target: { value: "Lesson 4 copies a textbook chapter." },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send report" }));
    expect(await screen.findByRole("status")).toHaveTextContent(
      "Thanks — report sent",
    );
    expect(request).toHaveBeenCalledWith("/v1/marketplace/reports", "POST", {
      target_type: "course",
      target_id: "course-1",
      reason: "copyright",
      text: "Lesson 4 copies a textbook chapter.",
    });
  });

  it("says so when the report merged into an earlier one", async () => {
    open();
    request.mockResolvedValueOnce({ status: "received", merged: true });
    fireEvent.click(screen.getByLabelText("Spam"));
    fireEvent.click(screen.getByRole("button", { name: "Send report" }));
    expect(await screen.findByRole("status")).toHaveTextContent(
      "already reported",
    );
  });

  it("keeps the draft on offline and rate-limit errors and retries once", async () => {
    open();
    request
      .mockRejectedValueOnce(new TypeError("offline"))
      .mockRejectedValueOnce(rejected(429))
      .mockResolvedValueOnce({ status: "received", merged: false });
    fireEvent.click(screen.getByLabelText("Other"));
    fireEvent.change(screen.getByLabelText("Details, optional"), {
      target: { value: "界 text" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send report" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Couldn't reach",
    );
    expect(screen.queryByText(/pq password/)).not.toBeInTheDocument();
    expect(screen.getByLabelText("Details, optional")).toHaveValue("界 text");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("Too many"),
    );
    expect(screen.getByLabelText("Other")).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("status")).toHaveTextContent("report sent");
    expect(request).toHaveBeenCalledTimes(3);
  });

  it("stays neutral when the target is gone and never sends twice at once", async () => {
    open();
    let release!: () => void;
    request.mockImplementationOnce(
      () => new Promise((_, reject) => (release = () => reject(rejected(404)))),
    );
    fireEvent.click(screen.getByLabelText("Spam"));
    const send = screen.getByRole("button", { name: "Send report" });
    fireEvent.click(send);
    fireEvent.click(send);
    release();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "isn't available",
    );
    expect(request).toHaveBeenCalledTimes(1);
  });

  it("bounds details at 500 characters by Unicode count", () => {
    open();
    fireEvent.click(screen.getByLabelText("Spam"));
    fireEvent.change(screen.getByLabelText("Details, optional"), {
      target: { value: "界".repeat(501) },
    });
    expect(screen.getByRole("button", { name: "Send report" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Details, optional"), {
      target: { value: "界".repeat(500) },
    });
    expect(screen.getByRole("button", { name: "Send report" })).toBeEnabled();
  });

  it("closes on Escape and the Close button and traps Tab", () => {
    const onClose = open();
    const dialog = screen.getByRole("dialog");
    const close = screen.getByRole("button", { name: "Close" });
    const details = screen.getByLabelText("Details, optional");
    close.focus();
    fireEvent.keyDown(dialog, { key: "Tab", shiftKey: true });
    expect(details).toHaveFocus(); // wraps from the first control to the last enabled one
    fireEvent.keyDown(dialog, { key: "Tab" });
    expect(close).toHaveFocus();
    fireEvent.keyDown(dialog, { key: "Escape" });
    fireEvent.click(close);
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("classifies failures by status only", () => {
    expect(reportFailure(rejected(404))).toBe("unavailable");
    expect(reportFailure(rejected(429))).toBe("rate_limited");
    expect(reportFailure(rejected(403))).toBe("self");
    expect(reportFailure(rejected(500))).toBe("offline");
    expect(reportFailure(new TypeError("x"))).toBe("offline");
  });
});

const detail = () => ({
  course_id: "c1",
  title: "Distributed systems",
  description: "d",
  category: "tech",
  language: "en",
  version: 3,
  published_at: "2026-11-02T00:00:00Z",
  creator: { username: "mina", display_name: "Mina" },
  lessons_ready: 6,
  lessons_total: 24,
  enrollment_count: 3,
  rating_average: null,
  rating_count: 0,
  tags: [],
  outline: [],
  sample_slides: [],
  viewer: { enrolled: false, owner: false },
});

describe("report entry points", () => {
  it("opens the course report from the More menu and restores focus", async () => {
    request.mockImplementation(async (path) =>
      String(path).endsWith("/reviews")
        ? {
            items: [],
            mine: null,
            lessons_needed: 0,
            completed_lessons: 3,
            min_lessons: 3,
            owner: false,
            can_review: false,
            rating_count: 0,
            rating_average: null,
          }
        : detail(),
    );
    render(<MarketCoursePage id="c1" />);
    const more = await screen.findByRole("button", {
      name: "More: report course",
    });
    fireEvent.click(more);
    fireEvent.click(
      screen.getByRole("menuitem", { name: "Report this course" }),
    );
    expect(
      screen.getByRole("dialog", { name: "Report this course" }),
    ).toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(more).toHaveFocus();
  });

  it("offers reporting the creator by public username", async () => {
    request.mockImplementation(async (path) =>
      String(path).endsWith("/reviews") ? { items: [] } : detail(),
    );
    render(<MarketCoursePage id="c1" />);
    fireEvent.click(
      await screen.findByRole("button", { name: "More: report course" }),
    );
    request.mockResolvedValueOnce({ status: "received", merged: false });
    fireEvent.click(screen.getByRole("menuitem", { name: "Report creator" }));
    fireEvent.click(screen.getByLabelText("Spam"));
    fireEvent.click(screen.getByRole("button", { name: "Send report" }));
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("/v1/marketplace/reports", "POST", {
        target_type: "user",
        target_id: "mina",
        reason: "spam",
        text: "",
      }),
    );
  });

  it("reports a single review without exposing author identity", async () => {
    request.mockResolvedValueOnce({
      items: [
        {
          id: "r1",
          stars: 2,
          text: "Rude",
          version: 1,
          verdict: "pass",
          author: "Kai",
        },
      ],
      mine: null,
      lessons_needed: 0,
      completed_lessons: 3,
      min_lessons: 3,
      owner: false,
      can_review: false,
      rating_count: 1,
      rating_average: 2,
    });
    render(<CourseReviews courseId="c1" />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Report review" }),
    );
    const dialog = screen.getByRole("dialog", { name: "Report this review" });
    expect(dialog).toBeInTheDocument();
    request.mockResolvedValueOnce({ status: "received", merged: false });
    fireEvent.click(screen.getByLabelText("Inappropriate content"));
    fireEvent.click(screen.getByRole("button", { name: "Send report" }));
    await waitFor(() =>
      expect(request).toHaveBeenLastCalledWith(
        "/v1/marketplace/reports",
        "POST",
        {
          target_type: "review",
          target_id: "r1",
          reason: "inappropriate",
          text: "",
        },
      ),
    );
  });
});
