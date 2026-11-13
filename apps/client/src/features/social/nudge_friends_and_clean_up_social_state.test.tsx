import { fireEvent, render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { NudgeControl, NudgeInbox } from "./Nudge";
import {
  createNudgeKeys,
  nudgeFailure,
  nudgeMessage,
  nudgeNote,
  nudgeState,
  unreadCount,
  type NudgeEntry,
} from "./nudges";
import type { FriendEntry } from "./friends";

const friend = (over: Partial<FriendEntry> = {}): FriendEntry => ({
  id: "f-1",
  username: "mina",
  display_name: "Mina",
  avatar: { initial: "M" },
  since: "2026-10-01T00:00:00Z",
  nudge: { sent: false },
  ...over,
});
const doneToday = {
  days: 3,
  longest: 3,
  state: "waiting_you" as const,
  you: {
    local_date: "2026-11-14",
    time_zone: "UTC",
    done: false,
    protected: false,
  },
  friend: {
    local_date: "2026-11-14",
    time_zone: "UTC",
    done: true,
    protected: false,
  },
};

describe("nudge state", () => {
  it("is available, sent for the day, or unnecessary once the friend finished", () => {
    expect(nudgeState(friend())).toBe("available");
    expect(
      nudgeState(
        friend({ nudge: { sent: true, next_at: "2026-11-15T00:00Z" } }),
      ),
    ).toBe("sent");
    expect(nudgeState(friend({ friend_streak: doneToday }))).toBe("finished");
    // Without a visible streak nothing is invented: the button stays available.
    expect(nudgeState(friend({ nudge: undefined }))).toBe("available");
    // Sent wins over finished: the day's nudge was spent.
    expect(
      nudgeState(friend({ friend_streak: doneToday, nudge: { sent: true } })),
    ).toBe("sent");
  });
  it("explains each state without revealing why a send was refused", () => {
    expect(nudgeNote("sent", "Mina")).toBe(
      "Nudge sent · you can nudge again tomorrow",
    );
    expect(nudgeNote("finished", "Mina")).toBe("Mina finished today");
    expect(nudgeNote("available", "Mina")).toBe("");
    expect(nudgeMessage("limited", "Mina")).toBe(
      "You already nudged Mina today. You can nudge again tomorrow.",
    );
    const unavailable = nudgeMessage("unavailable", "Mina");
    expect(unavailable).toBe("We couldn't send that nudge.");
    expect(unavailable).not.toMatch(/block|removed|deleted|setting/i);
    expect(nudgeMessage("offline", "Mina")).toMatch(/connection/i);
  });
  it("classifies failures by status and stable code, never by server text", () => {
    expect(nudgeFailure({ status: 429, code: "nudge_limited" })).toBe(
      "limited",
    );
    expect(nudgeFailure({ status: 429 })).toBe("limited");
    expect(nudgeFailure({ status: 404, code: "nudge_unavailable" })).toBe(
      "unavailable",
    );
    expect(nudgeFailure({ status: 404, details: { code: "x" } })).toBe(
      "unavailable",
    );
    expect(nudgeFailure(new TypeError("Failed to fetch"))).toBe("offline");
    expect(nudgeFailure({ status: 503 })).toBe("offline");
  });
});

describe("operation keys", () => {
  it("are stable for a retry and fresh after success", () => {
    let n = 0;
    const keys = createNudgeKeys(() => `key-${++n}`);
    expect(keys.keyFor("f-1")).toBe("key-1");
    expect(keys.keyFor("f-1")).toBe("key-1");
    expect(keys.keyFor("f-2")).toBe("key-2");
    keys.clear("f-1");
    expect(keys.keyFor("f-1")).toBe("key-3");
  });
});

describe("NudgeControl", () => {
  it("sends once per tap and is disabled while busy", () => {
    const onNudge = vi.fn();
    const { rerender } = render(
      <NudgeControl entry={friend()} busy={false} onNudge={onNudge} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Nudge Mina" }));
    expect(onNudge).toHaveBeenCalledTimes(1);
    rerender(<NudgeControl entry={friend()} busy onNudge={onNudge} />);
    expect(
      (screen.getByRole("button", { name: "Nudge Mina" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });
  it("replaces the button with the sent note and offers no second nudge", () => {
    render(
      <NudgeControl
        entry={friend({ nudge: { sent: true } })}
        busy={false}
        onNudge={vi.fn()}
      />,
    );
    expect(screen.queryByRole("button")).toBeNull();
    expect(
      screen.getByText("Nudge sent · you can nudge again tomorrow"),
    ).toBeTruthy();
  });
  it("shows no button when the friend already finished", () => {
    render(
      <NudgeControl
        entry={friend({ friend_streak: doneToday })}
        busy={false}
        onNudge={vi.fn()}
      />,
    );
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByText("Mina finished today")).toBeTruthy();
  });
  it("isolates a right-to-left name", () => {
    render(
      <NudgeControl
        entry={friend({ display_name: "ليلى" })}
        busy={false}
        onNudge={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: "Nudge ليلى" })).toBeTruthy();
    expect(document.querySelector("bdi")?.textContent).toBe("ليلى");
  });
});

describe("NudgeInbox", () => {
  const item = (over: Partial<NudgeEntry> = {}): NudgeEntry => ({
    id: "n-1",
    username: "mina",
    display_name: "Mina",
    avatar: { initial: "M" },
    created_at: "2026-11-14T10:00:00Z",
    read: false,
    ...over,
  });
  it("lists who nudged with new items marked and counts unread", () => {
    const items = [
      item(),
      item({ id: "n-2", username: "kai", display_name: "Kai", read: true }),
    ];
    expect(unreadCount(items)).toBe(1);
    render(<NudgeInbox items={items} />);
    const list = screen.getByRole("list", { name: "Nudges" });
    expect(list.textContent).toContain("Mina");
    expect(list.textContent).toContain("nudged you");
    expect(list.textContent).toContain("@kai");
    expect(screen.getAllByText("New")).toHaveLength(1);
  });
  it("has an empty state and tolerates a bad date", () => {
    const { rerender } = render(<NudgeInbox items={[]} />);
    expect(screen.getByText("No nudges right now.")).toBeTruthy();
    expect(screen.queryByRole("list")).toBeNull();
    rerender(<NudgeInbox items={[item({ created_at: "nope" })]} />);
    expect(screen.getByRole("list", { name: "Nudges" })).toBeTruthy();
  });
});
