import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ModeratorQueue } from "./ModeratorQueue";
import { ModerationNotices } from "./ModerationNotices";
import {
  actionsFor,
  auditWords,
  isSevereAction,
  moderationFailure,
  reasonCodes,
  severityLabel,
} from "./moderation";
import { session } from "../accounts/session";

vi.mock("../accounts/session", () => ({
  session: {
    request: vi.fn(),
    current: vi.fn(() => ({ id: "author-1" })),
    subscribe: vi.fn(() => () => {}),
  },
}));
const request = vi.mocked(session.request);
const rejected = (status: number) =>
  Object.assign(new Error("server text: pq password"), { status });
beforeEach(() => request.mockReset());

const course = {
  id: "item-1",
  target_type: "course",
  target_id: "course-1",
  course_id: "course-1",
  title: "Home chemistry",
  severity: 100,
  reasons: ["unsafe"],
  report_count: 4,
  opened_at: "2026-11-01T00:00:00Z",
  due_at: "2026-11-03T00:00:00Z",
  overdue: true,
  hidden: true,
  version: 2,
  audit: [{ at: "2026-11-02T08:02:00Z", action: "warn", reason_code: "spam" }],
};
const held = {
  ...course,
  id: "item-2",
  target_type: "publication",
  title: "Lockpicking 101",
  severity: 90,
  reasons: [],
  report_count: 0,
  overdue: false,
  hidden: false,
  screening: {
    verdict: "needs_review",
    codes: ["harmful_content"],
    sampled_lessons: 8,
  },
};
const view = (items: unknown[], extra = {}) => ({
  items,
  appeals: [],
  open_count: items.length,
  overdue_count: items.filter((i) => (i as { overdue: boolean }).overdue)
    .length,
  screening_count: items.filter((i) => (i as { screening?: unknown }).screening)
    .length,
  ...extra,
});

describe("moderation vocabulary", () => {
  it("labels severity at the configured boundaries", () => {
    expect([100, 80, 79, 40, 39, 0].map(severityLabel)).toEqual([
      "High",
      "High",
      "Medium",
      "Medium",
      "Low",
      "Low",
    ]);
  });

  it("matches the API action matrix per target", () => {
    expect(actionsFor("course")).toEqual([
      "dismiss",
      "warn",
      "delist",
      "takedown",
      "ban_publishing",
    ]);
    expect(actionsFor("publication")).toEqual([
      "dismiss",
      "delist",
      "takedown",
      "ban_publishing",
    ]);
    expect(actionsFor("review")).toEqual([
      "dismiss",
      "warn",
      "delist",
      "takedown",
    ]);
    expect(actionsFor("user")).toEqual(["dismiss", "warn", "ban_publishing"]);
    expect(actionsFor("lesson")).toEqual([]);
  });

  it("is severe only for a takedown with an unsafe or legal reason", () => {
    expect(isSevereAction("takedown", "unsafe")).toBe(true);
    expect(isSevereAction("takedown", "legal")).toBe(true);
    expect(isSevereAction("takedown", "spam")).toBe(false);
    expect(isSevereAction("delist", "unsafe")).toBe(false);
    expect(reasonCodes.map((r) => r.id)).toEqual([
      "unsafe",
      "legal",
      "inappropriate",
      "spam",
      "copyright",
      "misleading",
      "other",
      "no_violation",
    ]);
  });

  it("maps failures to fixed text and never shows server text", () => {
    const texts = [404, 403, 409, 429, 400, 500].map((s) =>
      moderationFailure(rejected(s)),
    );
    expect(new Set(texts).size).toBeGreaterThan(3);
    for (const text of [
      ...texts,
      moderationFailure(new TypeError("offline")),
    ]) {
      expect(text).not.toMatch(/pq|password|server text/);
      expect(text.length).toBeGreaterThan(5);
    }
    expect(moderationFailure(new TypeError("offline"))).toContain(
      "Couldn't reach Duolify",
    );
  });

  it("maps audit actions to human words including restored share links", () => {
    expect(auditWords.restore_link).toBe("Share link restored");
    expect(auditWords.dismiss).toBe("Dismissed");
    expect(auditWords.warn).toBe("Warned");
    expect(auditWords.delist).toBe("Delisted");
    expect(auditWords.takedown).toBe("Took down");
    expect(auditWords.ban_publishing).toBe("Banned publishing");
  });
});

describe("moderator queue", () => {
  it("lists items, counts and the overdue state, then opens the detail", async () => {
    request.mockResolvedValueOnce(view([course, held]));
    render(<ModeratorQueue />);
    expect(
      screen.getByRole("status", { name: "Loading queue" }),
    ).toBeInTheDocument();
    const row = await screen.findByRole("button", {
      name: "Course · Home chemistry",
    });
    expect(screen.getByText("2 open")).toBeInTheDocument();
    expect(screen.getByText("1 over 48 h")).toBeInTheDocument();
    expect(screen.getByText("1 awaiting screening review")).toBeInTheDocument();
    fireEvent.click(row);
    const detail = screen.getByRole("region", { name: "Report detail" });
    expect(
      within(detail).getByText("4 reporters · unsafe"),
    ).toBeInTheDocument();
    expect(
      within(detail).getByText("Auto-hidden from listings"),
    ).toBeInTheDocument();
    expect(within(detail).getByText("Past 48 h target")).toBeInTheDocument();
    expect(within(detail).getByText(/Warned/)).toBeInTheDocument();
  });

  it("renders a restored share link entry in the audit log", async () => {
    const courseWithRestore = {
      ...course,
      audit: [
        {
          at: "2026-11-02T08:02:00Z",
          action: "restore_link",
          reason_code: "no_violation",
        },
      ],
    };
    request.mockResolvedValueOnce(view([courseWithRestore]));
    render(<ModeratorQueue />);
    const row = await screen.findByRole("button", {
      name: "Course · Home chemistry",
    });
    fireEvent.click(row);
    const detail = screen.getByRole("region", { name: "Report detail" });
    expect(within(detail).getByText(/Share link restored/)).toBeInTheDocument();
  });

  it("filters to screening items and shows an empty state", async () => {
    request.mockResolvedValueOnce(view([course, held]));
    render(<ModeratorQueue />);
    await screen.findByRole("button", { name: "Course · Home chemistry" });
    fireEvent.click(screen.getByRole("button", { name: "Screening" }));
    expect(
      screen.queryByRole("button", { name: "Course · Home chemistry" }),
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: "Course · Lockpicking 101" }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Users" }));
    expect(
      screen.getByText("Nothing is waiting for review"),
    ).toBeInTheDocument();
  });

  it("requires a reason, sends the action and drops the resolved item", async () => {
    request.mockResolvedValueOnce(view([course]));
    render(<ModeratorQueue />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Course · Home chemistry" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Take down" }));
    const dialog = screen.getByRole("dialog", { name: "Take down course" });
    expect(within(dialog).getByLabelText("Reason code")).toHaveFocus();
    const confirm = within(dialog).getByRole("button", {
      name: "Confirm action",
    });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText("Reason code"), {
      target: { value: "unsafe" },
    });
    const block = within(dialog).getByLabelText("Also block existing copies");
    expect(block).toBeChecked();
    expect(block).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText("Reason sent to owner"), {
      target: { value: "  Lesson 3 is harmful.  " },
    });
    request.mockResolvedValueOnce({
      id: "a1",
      action: "takedown",
      severe: true,
      copies_blocked: 2,
    });
    fireEvent.click(confirm);
    expect(await screen.findByRole("status", { name: "" })).toHaveTextContent(
      "Action recorded",
    );
    expect(request).toHaveBeenLastCalledWith(
      "/v1/moderation/items/item-1/actions",
      "POST",
      {
        action: "takedown",
        reason_code: "unsafe",
        reason: "Lesson 3 is harmful.",
      },
    );
    expect(
      screen.queryByRole("button", { name: "Course · Home chemistry" }),
    ).toBeNull();
  });

  it("does not block copies for a spam delist and keeps the draft on failure", async () => {
    request.mockResolvedValueOnce(view([course]));
    render(<ModeratorQueue />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Course · Home chemistry" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Delist" }));
    const dialog = screen.getByRole("dialog", { name: "Delist course" });
    fireEvent.change(within(dialog).getByLabelText("Reason code"), {
      target: { value: "spam" },
    });
    expect(
      within(dialog).getByLabelText("Also block existing copies"),
    ).not.toBeChecked();
    fireEvent.change(within(dialog).getByLabelText("Reason sent to owner"), {
      target: { value: "Promotional." },
    });
    request.mockRejectedValueOnce(rejected(500));
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Confirm action" }),
    );
    expect(await within(dialog).findByRole("alert")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Reason sent to owner")).toHaveValue(
      "Promotional.",
    );
    expect(request).toHaveBeenCalledTimes(2);
  });

  it("closes the dialog on Escape and returns focus to the action", async () => {
    request.mockResolvedValueOnce(view([held]));
    render(<ModeratorQueue />);
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Course · Lockpicking 101",
      }),
    );
    const approve = screen.getByRole("button", { name: "Approve publication" });
    fireEvent.click(approve);
    fireEvent.keyDown(
      screen.getByRole("dialog", { name: "Approve publication" }),
      { key: "Escape" },
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(approve).toHaveFocus();
  });

  it("shows a neutral page for a non-moderator and a retry for network errors", async () => {
    request.mockRejectedValueOnce(rejected(404));
    const { unmount } = render(<ModeratorQueue />);
    expect(
      await screen.findByRole("heading", { name: "This page isn't available" }),
    ).toBeInTheDocument();
    unmount();
    request
      .mockRejectedValueOnce(new TypeError("offline"))
      .mockResolvedValueOnce(view([]));
    render(<ModeratorQueue />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Couldn't reach Duolify",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(
      await screen.findByText("Nothing is waiting for review"),
    ).toBeInTheDocument();
  });

  it("lists open appeals and decides one", async () => {
    request.mockResolvedValueOnce(
      view([], {
        appeals: [
          {
            id: "ap-1",
            action_id: "a1",
            course_id: "c",
            title: "Home chemistry",
            action: "takedown",
            reason_code: "unsafe",
            text: "The lessons are safe.",
            created_at: "2026-11-02T00:00:00Z",
          },
        ],
      }),
    );
    render(<ModeratorQueue />);
    const appeal = await screen.findByRole("article", {
      name: "Appeal: Home chemistry",
    });
    expect(appeal).toHaveTextContent("The lessons are safe.");
    fireEvent.click(within(appeal).getByRole("button", { name: "Overturn" }));
    const dialog = screen.getByRole("dialog", { name: "Overturn appeal" });
    fireEvent.change(within(dialog).getByLabelText("Reason sent to owner"), {
      target: { value: "Content is fine." },
    });
    request.mockResolvedValueOnce(undefined);
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Confirm action" }),
    );
    await waitFor(() =>
      expect(request).toHaveBeenLastCalledWith(
        "/v1/moderation/appeals/ap-1/decision",
        "POST",
        { decision: "overturn", reason: "Content is fine." },
      ),
    );
  });
});

describe("author notices and one appeal", () => {
  const notice = {
    id: "act-1",
    action: "delist",
    reason_code: "misleading",
    reason: "The title promises more than the lessons deliver.",
    target_type: "course",
    course_id: "c",
    title: "Home chemistry",
    severe: false,
    created_at: "2026-11-02T00:00:00Z",
    can_appeal: true,
  };

  it("shows the reason and sends exactly one appeal", async () => {
    request.mockResolvedValueOnce([notice]);
    render(<ModerationNotices />);
    const card = await screen.findByRole("article", {
      name: "Notice: course delisted",
    });
    expect(card).toHaveTextContent(
      "The title promises more than the lessons deliver.",
    );
    fireEvent.click(
      within(card).getByRole("button", { name: "Appeal decision" }),
    );
    const dialog = screen.getByRole("dialog", { name: "Appeal this decision" });
    const send = within(dialog).getByRole("button", { name: "Send appeal" });
    expect(send).toBeDisabled();
    fireEvent.change(
      within(dialog).getByLabelText("Why should this be reviewed?"),
      { target: { value: "  The lessons match.  " } },
    );
    request.mockResolvedValueOnce({ id: "ap-1" });
    fireEvent.click(send);
    expect(await screen.findByRole("status")).toHaveTextContent("Appeal sent");
    expect(request).toHaveBeenLastCalledWith(
      "/v1/moderation/notices/act-1/appeal",
      "POST",
      { text: "The lessons match." },
    );
    expect(within(card).getByText("Appeal under review")).toBeInTheDocument();
    expect(
      within(card).queryByRole("button", { name: "Appeal decision" }),
    ).toBeNull();
  });

  it("shows a used appeal, a blocked-copies warning and the empty state", async () => {
    request.mockResolvedValueOnce([
      {
        ...notice,
        can_appeal: false,
        appeal: { status: "upheld" },
        action: "takedown",
        severe: true,
      },
    ]);
    const { unmount } = render(<ModerationNotices />);
    const card = await screen.findByRole("article", {
      name: "Notice: course taken down",
    });
    expect(card).toHaveTextContent("Appeal upheld");
    expect(card).toHaveTextContent("Existing copies were blocked");
    expect(
      within(card).queryByRole("button", { name: "Appeal decision" }),
    ).toBeNull();
    unmount();
    request.mockResolvedValueOnce([]);
    render(<ModerationNotices />);
    expect(
      await screen.findByText("No moderation notices"),
    ).toBeInTheDocument();
  });

  it("keeps the appeal text when sending fails and says a second appeal is refused", async () => {
    request.mockResolvedValueOnce([notice]);
    render(<ModerationNotices />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Appeal decision" }),
    );
    const dialog = screen.getByRole("dialog", { name: "Appeal this decision" });
    fireEvent.change(
      within(dialog).getByLabelText("Why should this be reviewed?"),
      { target: { value: "Please look again." } },
    );
    request
      .mockRejectedValueOnce(new TypeError("offline"))
      .mockRejectedValueOnce(rejected(409));
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Send appeal" }),
    );
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Couldn't reach Duolify",
    );
    expect(
      within(dialog).getByLabelText("Why should this be reviewed?"),
    ).toHaveValue("Please look again.");
    fireEvent.click(within(dialog).getByRole("button", { name: "Retry" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "already used",
    );
  });
});
