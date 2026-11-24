import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { GuidedDailyGoal } from "../accounts/GuidedDailyGoal";
import { GuidedProfileEdit } from "../accounts/GuidedProfileEdit";
import { emptyContext, emptySettings } from "../accounts/learner-context";
import { profileApi } from "../accounts/learner-context";
import { session } from "../accounts/session";
import { GuidedAddFriend } from "../social/GuidedAddFriend";
import { GuidedUsername } from "../social/GuidedUsername";
import { trackGuidedChatEvent } from "../../lib/analytics";

vi.mock("../../lib/analytics", () => ({
  trackGuidedChatEvent: vi.fn(),
}));
vi.mock("../accounts/session", async (load) => {
  const actual = await load<typeof import("../accounts/session")>();
  return { ...actual, session: { ...actual.session, request: vi.fn() } };
});
vi.mock("../accounts/learner-context", async (load) => {
  const actual = await load<typeof import("../accounts/learner-context")>();
  return {
    ...actual,
    profileApi: { ...actual.profileApi, saveSettings: vi.fn() },
  };
});

beforeEach(() => {
  localStorage.clear();
  vi.clearAllMocks();
});

const type = (name: RegExp, value: string) => {
  fireEvent.change(screen.getByRole("textbox", { name }), {
    target: { value },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
};

describe("profile.edit", () => {
  it("opens on the saved profile and saves the changed fields once", async () => {
    const commit = vi.fn().mockResolvedValue(emptyContext());
    render(
      <GuidedProfileEdit
        profile={{
          ...emptyContext(),
          goal: "Order food abroad",
          topic: "Italian",
          background: "Two years at school",
          level: "beginner",
        }}
        onCommit={commit}
      />,
    );
    expect(await screen.findByText("Save these?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await waitFor(() =>
      expect(commit).toHaveBeenCalledWith("save", {
        goal: "Order food abroad",
        topic: "Italian",
        background: "Two years at school",
        level: "beginner",
      }),
    );
    expect(commit).toHaveBeenCalledTimes(1);
    expect(await screen.findByText("Profile saved.")).toBeTruthy();
    const events = vi
      .mocked(trackGuidedChatEvent)
      .mock.calls.map(([e]) => [e.scriptId, e.entryPoint, e.event]);
    expect(events).toContainEqual([
      "profile.edit",
      "profile",
      "guided_chat_resumed",
    ]);
    expect(events).toContainEqual([
      "profile.edit",
      "profile",
      "guided_chat_completed",
    ]);
  });
});

describe("profile.goal", () => {
  it("shows the reminder time on a labelled time field and saves through settings", async () => {
    const saved = {
      ...emptySettings(),
      daily_minutes: 20,
      daily_xp: 60,
      reminder_enabled: true,
      reminder_time: "07:30",
    };
    vi.mocked(profileApi.saveSettings).mockResolvedValue(saved);
    const onSaved = vi.fn();
    render(
      <GuidedDailyGoal settings={saved} onSaved={onSaved} onClose={vi.fn()} />,
    );
    await screen.findByText("Save your daily goal?");
    fireEvent.click(
      screen.getByRole("button", { name: /Change answer: 07:30/ }),
    );
    await waitFor(() =>
      expect(document.querySelector('input[type="time"]')).toBeTruthy(),
    );
    const clock =
      document.querySelector<HTMLInputElement>('input[type="time"]')!;
    expect(clock.getAttribute("aria-labelledby")).toBeTruthy();
    fireEvent.change(clock, { target: { value: "08:15" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    fireEvent.click(await screen.findByRole("button", { name: "Confirm" }));
    await waitFor(() =>
      expect(profileApi.saveSettings).toHaveBeenCalledWith({
        daily_minutes: 20,
        daily_xp: 60,
        reminder_enabled: true,
        reminder_time: "08:15",
      }),
    );
    expect(onSaved).toHaveBeenCalled();
    expect(await screen.findByText(/Daily goal saved/)).toBeTruthy();
  });
});

describe("social.username", () => {
  it("rejects a bad shape in the chat and claims a good name with PUT", async () => {
    vi.mocked(session.request).mockResolvedValue({
      username: null,
      rules: { min: 3, max: 20 },
    });
    const onSaved = vi.fn();
    render(<GuidedUsername method="PUT" onSaved={onSaved} />);
    type(/username/i, "bad__name");
    expect(await screen.findByText(/Use 3–20 characters/)).toBeTruthy();
    expect(session.request).not.toHaveBeenCalled();
    type(/username/i, "fresh_name");
    fireEvent.click(
      await screen.findByRole("button", { name: "Save username" }),
    );
    await waitFor(() =>
      expect(session.request).toHaveBeenCalledWith(
        "/v1/social/username",
        "PUT",
        { username: "fresh_name" },
      ),
    );
    expect(onSaved).toHaveBeenCalled();
  });

  it("shows the server's refusal when the name is taken", async () => {
    vi.mocked(session.request).mockRejectedValue(new Error("Already taken."));
    render(<GuidedUsername method="PATCH" onSaved={vi.fn()} />);
    type(/username/i, "taken_name");
    fireEvent.click(
      await screen.findByRole("button", { name: "Save username" }),
    );
    expect(await screen.findByText("Already taken.")).toBeTruthy();
  });
});

describe("social.add_friend", () => {
  it("stays neutral on a miss and sends one request on a hit", async () => {
    const request = vi.mocked(session.request);
    request.mockImplementation(async (path: string) => {
      if (path.includes("q=nobody")) return { results: [] };
      if (path.includes("q=maria_k"))
        return {
          results: [
            {
              username: "Maria_K",
              display_name: "Maria",
              avatar: { initial: "M" },
            },
          ],
        };
      return { status: "pending" };
    });
    render(<GuidedAddFriend />);
    type(/username/i, "nobody");
    expect(
      await screen.findByText("We couldn't find that username."),
    ).toBeTruthy();
    type(/username/i, "@Maria_K");
    fireEvent.click(
      await screen.findByRole("button", { name: "Send request" }),
    );
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "/v1/social/friend-requests",
        "POST",
        { username: "Maria_K" },
      ),
    );
    expect(await screen.findByText("Request sent.")).toBeTruthy();
  });
});
