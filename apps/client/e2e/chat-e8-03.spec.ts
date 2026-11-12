import type { Page } from "@playwright/test";
import { test, expect } from "./accounts-fixture";

const courseId = "6f1d2b9e-0c3a-4b7e-9d1a-2f4c8e5a7b03";
const sse = (...events: [string, unknown][]) =>
  events
    .map(([name, data]) => `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`)
    .join("");

type Calls = {
  quotes: Record<string, unknown>[];
  sent: Record<string, unknown>[];
};

/** A large course: the first quote needs approval, the approved one succeeds. */
async function mockLargeCourse(page: Page, mode: "approval" | "too_large") {
  const state: Calls = { quotes: [], sent: [] };
  await page.route(`**/v1/course/${courseId}`, (route) =>
    route.fulfill({
      json: {
        id: courseId,
        title: "Astronomy",
        description: "Look up",
        units: [
          {
            title: "Sky",
            lessons: [{ id: "l1", title: "Moon", state: "current" }],
          },
        ],
      },
    }),
  );
  await page.route("**/v1/me/credits", (route) =>
    route.fulfill({
      json: {
        available: 152,
        daily_free: {
          available: 28,
          granted: 30,
          resets_at: "2030-01-02T00:00:00Z",
          timezone: "UTC",
        },
        purchased: 124,
        subscription: 0,
        reserved: 0,
        recent_activity: [],
      },
    }),
  );
  await page.route(`**/v1/course/${courseId}/tutor/thread`, (route) =>
    route.fulfill({ json: { thread: null, messages: [] } }),
  );
  await page.route(`**/v1/course/${courseId}/tutor/quote`, (route) => {
    const body = route.request().postDataJSON() as Record<string, unknown>;
    state.quotes.push(body);
    if (mode === "too_large")
      return route.fulfill({
        status: 422,
        json: { error: "context_too_large", prompt_limit_tokens: 48000 },
      });
    if (body.approve_large_context !== true)
      return route.fulfill({
        status: 422,
        json: {
          error: "context_approval_required",
          required_tokens: 20000,
          standard_limit_tokens: 12500,
          prompt_limit_tokens: 48000,
        },
      });
    return route.fulfill({
      json: {
        id: "quote-approved",
        content_ceiling: 5,
        pricing_version: "unapproved-test",
        daily_free_eligible: true,
        expires_at: new Date(Date.now() + 600000).toISOString(),
      },
    });
  });
  await page.route("**/v1/quotes/*/accept", (route) =>
    route.fulfill({ json: { accepted: true } }),
  );
  await page.route(`**/v1/course/${courseId}/tutor/messages`, async (route) => {
    state.sent.push(route.request().postDataJSON() as Record<string, unknown>);
    await route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: sse(
        ["thread", { thread_id: "t1" }],
        [
          "delta",
          {
            text:
              "Zorblax is the founding constant of lesson one. " +
              "Very long answer ".repeat(60),
          },
        ],
        ["done", { message_id: "m-done", thread_id: "t1", replayed: false }],
      ),
    });
  });
  return state;
}
const panel = (page: Page) =>
  page.getByRole("complementary", { name: "Tutor chat" });
const noOverflow = (page: Page) =>
  page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);

for (const colorScheme of ["light", "dark"] as const)
  for (const mobile of [false, true])
    test.describe(`oldest lesson in a large course ${mobile ? "mobile touch" : "desktop keyboard"} ${colorScheme}`, () => {
      test.use({ hasTouch: mobile });
      test.beforeEach(async ({ page }) => {
        await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
        if (mobile) await page.setViewportSize({ width: 390, height: 780 });
      });

      test("explicit approval, then a grounded answer about the oldest lesson", async ({
        page,
      }) => {
        const calls = await mockLargeCourse(page, "approval");
        await page.goto(`/course/${courseId}`);
        await page.getByRole("button", { name: "Ask the tutor" }).click();
        const input = page.getByRole("textbox", { name: "Message" });
        await input.fill("What is zorblax from the very first lesson?");
        const alert = panel(page).getByRole("alert");
        await expect(alert).toContainText(/larger context/i);
        await expect(alert).toContainText(/20,000/);
        expect(await noOverflow(page)).toBe(true);
        // Nothing is sent without approval.
        await input.press("Enter");
        expect(calls.sent).toHaveLength(0);
        const approve = panel(page).getByRole("button", {
          name: "Approve larger context",
        });
        if (mobile) await approve.tap();
        else {
          await approve.focus();
          await page.keyboard.press("Enter");
        }
        await expect(page.getByText(/up to 5 credits/i)).toBeVisible();
        await expect(input).toBeFocused();
        expect(calls.quotes.at(-1)).toMatchObject({
          approve_large_context: true,
        });
        await input.press("Enter");
        await expect(
          page.getByText(/founding constant of lesson one/),
        ).toBeVisible();
        expect(calls.sent[0]).toMatchObject({ quote_id: "quote-approved" });
        expect(await noOverflow(page)).toBe(true);
      });

      test("a course above the ceiling shows a typed recovery, keeps the draft and charges nothing", async ({
        page,
      }) => {
        const calls = await mockLargeCourse(page, "too_large");
        await page.goto(`/course/${courseId}`);
        await page.getByRole("button", { name: "Ask the tutor" }).click();
        const input = page.getByRole("textbox", { name: "Message" });
        await input.fill("Summarise everything");
        await expect(panel(page).getByRole("alert")).toContainText(
          /too large/i,
        );
        await expect(panel(page).getByRole("alert")).toContainText(
          /Nothing was charged/i,
        );
        await expect(
          panel(page).getByRole("button", { name: "Approve larger context" }),
        ).toHaveCount(0);
        await input.press("Enter");
        expect(calls.sent).toHaveLength(0);
        await expect(input).toHaveValue("Summarise everything");
        expect(await noOverflow(page)).toBe(true);
      });
    });
