import type { Page } from "@playwright/test";
import { test, expect } from "./accounts-fixture";

type Side = {
  local_date: string;
  time_zone: string;
  done: boolean;
  protected: boolean;
};
type Streak = {
  days: number;
  longest: number;
  state: string;
  you: Side;
  friend: Side;
  checking_until?: string;
};

const you = (over: Partial<Side> = {}): Side => ({
  local_date: "2026-11-14",
  time_zone: "Pacific/Auckland",
  done: true,
  protected: false,
  ...over,
});
const mina = (over: Partial<Side> = {}): Side => ({
  local_date: "2026-11-13",
  time_zone: "America/Los_Angeles",
  done: false,
  protected: false,
  ...over,
});

function friend(name: string, streak?: Streak) {
  return {
    id: `f-${name}`,
    username: name.toLowerCase(),
    display_name: name,
    avatar: { initial: name[0] },
    since: "2026-10-01T00:00:00Z",
    streak: streak ? { days: streak.days } : undefined,
    friend_streak: streak,
  };
}

async function mockSocial(page: Page, friends: unknown[]) {
  await page.route("**/v1/social/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/identity"))
      return route.fulfill({
        json: {
          username: {
            username: "you",
            display_name: "You",
            avatar: { initial: "Y" },
          },
          rules: { min: 3, max: 20 },
        },
      });
    if (path.endsWith("/friends")) return route.fulfill({ json: { friends } });
    if (path.endsWith("/friend-requests"))
      return route.fulfill({ json: { incoming: [], outgoing: [] } });
    if (path.endsWith("/blocks"))
      return route.fulfill({ json: { blocks: [] } });
    return route.fulfill({ json: {} });
  });
}

for (const mobile of [false, true]) {
  const label = mobile ? "mobile" : "desktop";
  test(`friend streak grows across two time zones ${label}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });
    await mockSocial(page, [
      friend("Mina", {
        days: 4,
        longest: 4,
        state: "waiting_friend",
        you: you(),
        friend: mina(),
      }),
      friend("Kai", {
        days: 12,
        longest: 12,
        state: "both_done",
        you: you(),
        friend: mina({ done: true, time_zone: "Europe/Berlin" }),
      }),
      friend("Hidden"),
    ]);
    await page.goto("/friends");
    const rows = page.getByRole("list", { name: "Your friends" });
    await expect(rows).toContainText("Friend streak · 4 days");
    await expect(rows).toContainText("You: done");
    await expect(rows).toContainText("Mina: pending");
    await expect(rows).toContainText("Both done today");
    // No streak data (setting off or no streak): nothing is invented.
    await expect(
      rows.getByRole("listitem").filter({ hasText: "Hidden" }),
    ).not.toContainText("Friend streak");

    // Keyboard opens the detail with each friend's own local day.
    const open = page.getByRole("button", { name: "Open Mina" });
    await open.focus();
    await open.press("Enter");
    const detail = page.getByRole("region", { name: "Friend details" });
    await expect(detail).toContainText("4-day friend streak");
    await expect(detail).toContainText("2026-11-14 · Pacific/Auckland");
    await expect(detail).toContainText("2026-11-13 · America/Los_Angeles");
    await expect(detail).toContainText("Today counts once Mina finishes");
    await expect(detail.getByRole("button", { name: /buy/i })).toHaveCount(0);
  });

  test(`protect one and both missed sides ${label}`, async ({ page }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await mockSocial(page, [
      friend("Mina", {
        days: 7,
        longest: 7,
        state: "protected",
        you: you({ done: false, protected: true }),
        friend: mina({ done: true }),
      }),
      friend("Ana", {
        days: 7,
        longest: 9,
        state: "protected",
        you: you({ done: false, protected: true }),
        friend: mina({ done: false, protected: true }),
      }),
      friend("Leo", {
        days: 0,
        longest: 5,
        state: "broken",
        you: you(),
        friend: mina(),
      }),
      friend("Noa", {
        days: 3,
        longest: 3,
        state: "checking",
        you: you({ done: true }),
        friend: mina({ done: false }),
        checking_until: "2026-11-14T19:00:00Z",
      }),
    ]);
    await page.goto("/friends");
    const rows = page.getByRole("list", { name: "Your friends" });
    const mine = rows.getByRole("listitem").filter({ hasText: "Mina" });
    await expect(mine).toContainText("Your freeze covered today");
    const both = rows.getByRole("listitem").filter({ hasText: "Ana" });
    await expect(both).toContainText("Both freezes covered today");
    await expect(
      rows.getByRole("listitem").filter({ hasText: "Leo" }),
    ).toContainText("Friend streak ended · longest 5 days");
    await expect(
      rows.getByRole("listitem").filter({ hasText: "Noa" }),
    ).toContainText("Shared day checking");
  });
}
