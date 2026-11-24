import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { GuidedInvite } from "../sharing/GuidedInvite";
import { GuidedRepublish } from "../sharing/GuidedRepublish";
import { GuidedReview } from "../sharing/GuidedReview";
import { GuidedShare } from "../sharing/GuidedShare";
import { ReportSheet } from "../sharing/ReportSheet";
import { reportReasons } from "../sharing/reports";
import { QuotePanel } from "../pricing/QuotePanel";
import { quoteAPI } from "../pricing/quotes";
import { session } from "../accounts/session";
import { trackGuidedChatEvent } from "../../lib/analytics";
import { reportContent, shareScripts } from "./share.scripts";

vi.mock("../../lib/analytics", async (load) => ({
  ...(await load<typeof import("../../lib/analytics")>()),
  trackGuidedChatEvent: vi.fn(),
}));
vi.mock("../accounts/session", async (load) => {
  const actual = await load<typeof import("../accounts/session")>();
  return { ...actual, session: { ...actual.session, request: vi.fn() } };
});

beforeEach(() => {
  localStorage.clear();
  vi.clearAllMocks();
});

const send = () =>
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
const pick = (name: string) =>
  fireEvent.click(screen.getByRole("radio", { name }));
const press = async (name: string) =>
  fireEvent.click(await screen.findByRole("button", { name }));
const type = (name: RegExp, value: string) =>
  fireEvent.change(screen.getByRole("textbox", { name }), {
    target: { value },
  });

describe("scripts", () => {
  it("report reasons are exactly the closed list the API validates", () => {
    const ids = reportContent.steps[0].options!.map((o) => o.id);
    expect(ids).toEqual(reportReasons.map((r) => r.id));
  });
  it("no flow under three questions shows the last-one hint", () => {
    for (const s of shareScripts)
      expect(s.steps.filter((x) => x.widget !== "confirm").length).toBeLessThan(
        3,
      );
  });
});

describe("report.content", () => {
  it("sends the chosen reason and no details when skipped", async () => {
    vi.mocked(session.request).mockResolvedValue({
      status: "received",
      merged: false,
    });
    render(
      <ReportSheet
        target={{ type: "course", id: "c1" }}
        title="Report this course"
        onClose={vi.fn()}
      />,
    );
    await screen.findByText("What's wrong?");
    pick("Spam");
    send();
    await press("Skip");
    await press("Send report");
    await waitFor(() =>
      expect(session.request).toHaveBeenCalledWith(
        "/v1/marketplace/reports",
        "POST",
        { target_type: "course", target_id: "c1", reason: "spam", text: "" },
      ),
    );
    expect(await screen.findByText("Thanks — report sent")).toBeTruthy();
    expect(session.request).toHaveBeenCalledTimes(1);
  });

  it("shows the neutral failure as an alert and keeps the chat for a retry", async () => {
    vi.mocked(session.request).mockRejectedValue({ status: 429 });
    render(
      <ReportSheet
        target={{ type: "review", id: "r1" }}
        title="Report this review"
        onClose={vi.fn()}
      />,
    );
    await screen.findByText("What's wrong?");
    pick("Other");
    send();
    type(/details|add/i, "rude");
    send();
    await press("Send report");
    expect(
      await screen.findByText("Too many reports today. Try again tomorrow."),
    ).toBeTruthy();
    expect(screen.getByRole("alert")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
    const calls = vi.mocked(trackGuidedChatEvent).mock.calls.map(([e]) => e);
    expect(calls.every((e) => e.scriptId === "report.content")).toBe(true);
    expect(JSON.stringify(calls)).not.toContain("rude");
  });
});

describe("course.invite", () => {
  it("cleans the username and sends once after the confirm card", async () => {
    const onSend = vi.fn().mockResolvedValue(true);
    render(<GuidedInvite onSend={onSend} />);
    type(/username/i, " @Maria_K ");
    send();
    expect(onSend).not.toHaveBeenCalled();
    await press("Send invite");
    await waitFor(() => expect(onSend).toHaveBeenCalledWith("maria_k"));
    expect(await screen.findByText(/Invite sent/)).toBeTruthy();
    expect(onSend).toHaveBeenCalledTimes(1);
  });
  it("offers a retry when the invite is refused", async () => {
    const onSend = vi.fn().mockResolvedValue(false);
    render(<GuidedInvite onSend={onSend} />);
    type(/username/i, "maria_k");
    send();
    await press("Send invite");
    expect(
      await screen.findByRole("button", { name: "Try again" }),
    ).toBeTruthy();
  });
});

describe("course.republish", () => {
  it("publishes with the changelog after an explicit confirm", async () => {
    const onPublish = vi.fn().mockResolvedValue(true);
    render(<GuidedRepublish onPublish={onPublish} />);
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "Added unit 4" },
    });
    send();
    expect(onPublish).not.toHaveBeenCalled();
    await press("Publish");
    await waitFor(() => expect(onPublish).toHaveBeenCalledWith("Added unit 4"));
  });
  it("lets the changelog be skipped", async () => {
    const onPublish = vi.fn().mockResolvedValue(true);
    render(<GuidedRepublish onPublish={onPublish} />);
    await press("Skip");
    await press("Publish");
    await waitFor(() => expect(onPublish).toHaveBeenCalledWith(""));
  });
});

describe("course.review", () => {
  it("posts stars and an optional note, and opens on an existing review", async () => {
    const onSave = vi.fn().mockResolvedValue(true);
    const { unmount } = render(
      <GuidedReview mine={null} maxChars={2000} onSave={onSave} />,
    );
    pick("4 stars");
    send();
    await press("Skip");
    await press("Post review");
    await waitFor(() => expect(onSave).toHaveBeenCalledWith(4, ""));
    unmount();
    render(
      <GuidedReview
        mine={{ stars: 5, text: "Great" }}
        maxChars={2000}
        onSave={onSave}
      />,
    );
    expect(await screen.findByText("Post your review?")).toBeTruthy();
    expect(screen.getAllByText("Great").length).toBeGreaterThan(0);
  });
});

describe("course.share", () => {
  it("applies a change only after the confirm card and skips a no-op", async () => {
    const onChange = vi.fn().mockResolvedValue(true);
    render(<GuidedShare shared={false} onChange={onChange} />);
    await press("Change answer: Only me");
    pick("Anyone with the link");
    send();
    expect(onChange).not.toHaveBeenCalled();
    await press("Apply");
    await waitFor(() => expect(onChange).toHaveBeenCalledWith("link"));
  });
  it("makes no call when the answer matches the current visibility", async () => {
    const onChange = vi.fn();
    render(<GuidedShare shared={false} onChange={onChange} />);
    await press("Apply");
    expect(await screen.findByText(/Sharing updated/)).toBeTruthy();
    expect(onChange).not.toHaveBeenCalled();
  });
});

describe("credits.confirm", () => {
  it("shows the quoted price and accepts only on the explicit confirm", async () => {
    vi.spyOn(quoteAPI, "create").mockResolvedValue({
      id: "q1",
      pricing_version: "v1",
      action: "lesson_content",
      currency: "USD",
      content_ceiling: 140,
      audio_ceiling: 50,
      daily_free_eligible: false,
      expires_at: new Date(Date.now() + 600000).toISOString(),
      accepted: false,
    });
    const accept = vi
      .spyOn(quoteAPI, "accept")
      .mockResolvedValue({ accepted: true });
    const onAccepted = vi.fn();
    render(<QuotePanel consent={{ onAccepted, onCancel: vi.fn() }} />);
    expect(
      await screen.findByText("Content ceiling: 140 credits"),
    ).toBeTruthy();
    expect(screen.getByText("Audio ceiling: 50 credits")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Accept quote terms" })).toBe(
      null,
    );
    expect(accept).not.toHaveBeenCalled();
    await press("Accept terms");
    await waitFor(() => expect(accept).toHaveBeenCalledWith("q1"));
    expect(onAccepted).toHaveBeenCalledTimes(1);
  });
});
