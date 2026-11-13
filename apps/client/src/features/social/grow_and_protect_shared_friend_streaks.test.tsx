import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { FriendStreakDetail, FriendStreakRow } from "./FriendStreak";
import { describeStreak, type FriendStreak } from "./friendStreak";

const you = {
  local_date: "2026-11-14",
  time_zone: "Pacific/Auckland",
  done: true,
  protected: false,
};
const mina = {
  local_date: "2026-11-13",
  time_zone: "America/Los_Angeles",
  done: false,
  protected: false,
};
const streak = (over: Partial<FriendStreak> = {}): FriendStreak => ({
  days: 4,
  longest: 4,
  state: "waiting_friend",
  you,
  friend: mina,
  ...over,
});

describe("describeStreak", () => {
  it("shows each friend's own status and pluralises days", () => {
    expect(describeStreak("Mina", streak())).toMatchObject({
      headline: "Friend streak · 4 days",
      you: "You: done",
      friend: "Mina: pending",
    });
    expect(describeStreak("Mina", streak({ days: 1 })).headline).toBe(
      "Friend streak · 1 day",
    );
  });
  it("labels both-done, protected, ended and not-started states", () => {
    const both = streak({
      state: "both_done",
      friend: { ...mina, done: true },
    });
    expect(describeStreak("Mina", both).note).toBe("Both done today");
    const one = streak({
      state: "protected",
      you: { ...you, done: false, protected: true },
    });
    expect(describeStreak("Mina", one).note).toBe("Your freeze covered today");
    const friendOnly = streak({
      state: "protected",
      friend: { ...mina, protected: true },
    });
    expect(describeStreak("Mina", friendOnly).note).toBe(
      "Mina's freeze covered today",
    );
    const two = streak({
      state: "protected",
      you: { ...you, done: false, protected: true },
      friend: { ...mina, protected: true },
    });
    expect(describeStreak("Mina", two).note).toBe("Both freezes covered today");
    expect(
      describeStreak("Mina", streak({ days: 0, longest: 5, state: "broken" }))
        .headline,
    ).toBe("Friend streak ended · longest 5 days");
    expect(
      describeStreak("Mina", streak({ days: 0, longest: 0, state: "none" }))
        .headline,
    ).toBe("Start a friend streak");
  });
  it("does not finalise until the later day-end", () => {
    const checking = streak({
      state: "checking",
      checking_until: "2026-11-14T19:00:00Z",
    });
    expect(describeStreak("Mina", checking).note).toMatch(
      /^Shared day checking until \d{1,2}:\d{2}/,
    );
    expect(describeStreak("Mina", streak({ state: "checking" })).note).toBe(
      "Shared day checking",
    );
  });
});

describe("friend streak components", () => {
  it("row shows the summary with the friend name isolated for bidi text", () => {
    render(<FriendStreakRow name="ليلى" streak={streak()} />);
    expect(screen.getByText("Friend streak · 4 days")).toBeTruthy();
    expect(screen.getByText("You: done")).toBeTruthy();
    expect(document.querySelector("bdi")?.textContent).toContain("ليلى");
  });
  it("detail shows both local dates, the waiting copy and no way to buy protection", () => {
    render(<FriendStreakDetail name="Mina" streak={streak()} />);
    expect(
      screen.getByRole("heading", { name: "4-day friend streak" }),
    ).toBeTruthy();
    expect(screen.getByText("2026-11-14 · Pacific/Auckland")).toBeTruthy();
    expect(screen.getByText("2026-11-13 · America/Los_Angeles")).toBeTruthy();
    expect(screen.getByText(/Today counts once Mina finishes/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /buy|purchase/i })).toBeNull();
  });
});
