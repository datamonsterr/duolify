import type { Page } from "@playwright/test";
import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";

resetTokens();
test.afterEach(() => resetTokens());

const sse = (...events: [string, unknown][]) =>
  events
    .map(([name, data]) => `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`)
    .join("");
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

type Mode = "ok" | "zero_credits";
type State = {
  quotes: Record<string, unknown>[];
  sent: Record<string, unknown>[];
  stops: number;
};
type Posthog = { event: string; properties: Record<string, unknown> };

declare global {
  interface Window {
    tutorStream: {
      aborted: boolean;
      opened: number;
      push: (frame: string) => void;
      close: () => void;
    };
  }
}

/** Holds the first tutor SSE response open so the test decides when deltas arrive. */
async function holdStream(page: Page) {
  await page.addInitScript(() => {
    const real = window.fetch.bind(window);
    const encoder = new TextEncoder();
    let controller: ReadableStreamDefaultController<Uint8Array> | undefined;
    const handle = {
      aborted: false,
      opened: 0,
      push: (frame: string) => controller?.enqueue(encoder.encode(frame)),
      close: () => controller?.close(),
    };
    window.tutorStream = handle;
    window.fetch = (input, init) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (!url.endsWith("/tutor/messages") || init?.method !== "POST")
        return real(input, init);
      if (handle.opened >= 1) return real(input, init);
      handle.opened++;
      return new Promise<Response>((resolve, reject) => {
        const body = new ReadableStream<Uint8Array>({
          start(c) {
            controller = c;
          },
        });
        init?.signal?.addEventListener("abort", () => {
          handle.aborted = true;
          const error = new DOMException("aborted", "AbortError");
          try {
            controller?.error(error);
          } catch {
            /* already closed */
          }
          reject(error);
        });
        (handle as unknown as { open: () => void }).open = () =>
          resolve(
            new Response(body, {
              status: 200,
              headers: { "content-type": "text/event-stream" },
            }),
          );
      });
    };
  });
}
const openStream = async (page: Page) => {
  await page.waitForFunction(() => window.tutorStream.opened > 0);
  await page.evaluate(() =>
    (window.tutorStream as unknown as { open: () => void }).open(),
  );
};
const push = (page: Page, frame: string) =>
  page.evaluate((f) => window.tutorStream.push(f), frame);

async function mockTutor(page: Page, mode: Mode = "ok") {
  const state: State = { quotes: [], sent: [], stops: 0 };
  await page.route("**/v1/me/credits", (route) =>
    route.fulfill({
      json: {
        available: mode === "ok" ? 152 : 0,
        daily_free: {
          available: mode === "ok" ? 28 : 0,
          granted: 30,
          resets_at: "2030-01-02T00:00:00Z",
          timezone: "UTC",
        },
        purchased: mode === "ok" ? 124 : 0,
        subscription: 0,
        reserved: 0,
        recent_activity: [],
      },
    }),
  );
  await page.route("**/v1/quotes/*/accept", (route) =>
    route.fulfill({ json: { accepted: true } }),
  );
  await page.route("**/v1/courses/*/tutor/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith("/tutor/thread"))
      return route.fulfill({ json: { thread: null, messages: [] } });
    if (path.endsWith("/tutor/quote")) {
      state.quotes.push(request.postDataJSON());
      return route.fulfill({
        json: {
          id: "quote-lesson",
          content_ceiling: 5,
          pricing_version: "unapproved-test",
          daily_free_eligible: true,
          expires_at: new Date(Date.now() + 600000).toISOString(),
        },
      });
    }
    if (path.endsWith("/stop")) {
      state.stops++;
      return route.fulfill({
        json: { state: "stopped", status: "stopped", stopped: true },
      });
    }
    if (path.includes("/tutor/turns/"))
      return route.fulfill({ status: 404, json: { error: "not_found" } });
    if (path.endsWith("/tutor/messages")) {
      state.sent.push(request.postDataJSON());
      if (mode === "zero_credits")
        return route.fulfill({
          status: 402,
          json: {
            error: "insufficient_credits",
            draft_kept: true,
            next_grant_at: "2030-01-02T00:00:00Z",
          },
        });
      return route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: sse(
          ["thread", { thread_id: "t1" }],
          ["delta", { text: "Look at the second " }],
          ["delta", { text: "star on this slide." }],
          ["done", { message_id: "m-done", thread_id: "t1", replayed: false }],
        ),
      });
    }
    return route.continue();
  });
  return state;
}

async function recordPosthog(page: Page) {
  const events: Posthog[] = [];
  await page.route("**/e/**", (route) => {
    try {
      const raw = JSON.parse(route.request().postData() || "{}");
      events.push(...(Array.isArray(raw) ? raw : [raw]));
    } catch {
      /* not a capture payload */
    }
    return route.fulfill({ json: { status: 1 } });
  });
  return events;
}

async function seedLesson(
  page: Page,
  request: import("@playwright/test").APIRequestContext,
) {
  await page.goto("/profile");
  const tokens = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
  );
  const seeded = await request.post(`${apiBase}/test/player/fixture`, {
    headers: { Authorization: `Bearer ${tokens.access_token}` },
  });
  expect(seeded.status()).toBe(201);
  const { lesson_id } = await seeded.json();
  await page.goto(`/lesson/${lesson_id}?returnTo=%2Fcourses`);
  await expect(page.getByText("Slide 1 of 3")).toBeVisible();
  return lesson_id as string;
}

const noOverflow = (page: Page) =>
  page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);

async function activate(page: Page, mobile: boolean, name: string) {
  const button = page.getByRole("button", { name, exact: true });
  if (mobile) await button.tap();
  else {
    await button.focus();
    await page.keyboard.press("Enter");
  }
}

for (const colorScheme of ["light", "dark"] as const)
  for (const mobile of [false, true])
    test.describe(`lesson tutor ${mobile ? "mobile touch" : "desktop keyboard"} ${colorScheme}`, () => {
      test.use({ hasTouch: mobile });
      test.beforeEach(async ({ page }) => {
        await page.setViewportSize(
          mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
        );
        await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
      });

      test("opens paused with server-verified context, restores focus, slide and time on close", async ({
        page,
        request,
      }) => {
        const posthog = await recordPosthog(page);
        const state = await mockTutor(page);
        const lessonId = await seedLesson(page, request);
        const time = page.getByRole("slider", { name: "Slide time" });
        await time.fill("1200");
        await activate(page, mobile, "Ask tutor");
        const tutor = page
          .getByRole("region", { name: "Ask tutor" })
          .or(page.getByRole("dialog", { name: "Ask tutor" }));
        await expect(tutor).toBeVisible();
        await expect(tutor.getByText("Lesson paused")).toBeVisible();
        const input = tutor.getByRole("textbox", { name: "Message" });
        await expect(input).toBeFocused();
        await input.fill("What is this star?");
        await expect(tutor.getByText(/up to 5 credits/i)).toBeVisible();
        // Context is a reference only: the client never sends lesson text.
        expect(state.quotes.at(-1)).toMatchObject({
          lesson: { lesson_id: lessonId, slide_index: 0 },
        });
        expect(
          Object.keys(state.quotes.at(-1)!.lesson as object).sort(),
        ).toEqual(["lesson_id", "slide_index"]);
        await input.press("Enter");
        await expect(tutor.getByText(/star on this slide/)).toBeVisible();
        expect(state.sent[0]).toMatchObject({
          lesson: { lesson_id: lessonId, slide_index: 0 },
        });
        // Start and finish are announced once; replies are not read token by token.
        await expect(
          tutor.getByRole("status").filter({ hasText: "Tutor replied" }),
        ).toBeVisible();
        expect(await noOverflow(page)).toBe(true);
        // Close: focus returns to the opener; slide, time and paused state are untouched.
        await page.keyboard.press("Escape");
        await expect(tutor).toBeHidden();
        await expect(
          page.getByRole("button", { name: "Ask tutor", exact: true }),
        ).toBeFocused();
        await expect(page.getByText("Slide 1 of 3")).toBeVisible();
        await expect(time).toHaveValue("1200");
        await expect(
          page.getByRole("button", { name: "Play", exact: true }),
        ).toBeVisible();
        // Analytics: UUID distinct id, common properties, no private text.
        await expect
          .poll(() => posthog.some((e) => e.event === "ui_sheet_opened"))
          .toBe(true);
        const opened = posthog.find((e) => e.event === "ui_sheet_opened")!;
        expect(opened.properties).toMatchObject({
          sheet_id: "lesson_tutor",
          source: "client",
        });
        expect(String(opened.properties.distinct_id)).toMatch(uuid);
        expect(JSON.stringify(posthog)).not.toContain("What is this star?");
      });

      test("Stop is keyboard reachable; the log is not a live region while streaming", async ({
        page,
        request,
      }) => {
        await holdStream(page);
        const state = await mockTutor(page);
        await seedLesson(page, request);
        await activate(page, mobile, "Ask tutor");
        const tutor = page
          .getByRole("region", { name: "Ask tutor" })
          .or(page.getByRole("dialog", { name: "Ask tutor" }));
        const input = tutor.getByRole("textbox", { name: "Message" });
        await input.fill("Why does it twinkle?");
        await expect(tutor.getByText(/up to 5 credits/i)).toBeVisible();
        await input.press("Enter");
        await openStream(page);
        await push(
          page,
          sse(
            ["thread", { thread_id: "t1" }],
            ["delta", { text: "Because air" }],
          ),
        );
        await expect(tutor.getByText("Because air")).toBeVisible();
        await expect(tutor.getByRole("log")).toHaveAttribute(
          "aria-live",
          "off",
        );
        const status = tutor
          .getByRole("status")
          .filter({ hasText: "Tutor is answering" });
        await expect(status).toBeVisible();
        await expect(status).not.toContainText("Because air");
        const stop = tutor.getByRole("button", { name: "Stop reply" });
        if (mobile) await stop.tap();
        else {
          await page.keyboard.press("Tab");
          await stop.focus();
          await expect(stop).toBeFocused();
          await page.keyboard.press("Enter");
        }
        await expect(
          tutor.getByRole("status").filter({ hasText: "Stopped" }),
        ).toBeVisible();
        expect(state.stops).toBe(1);
        await expect(tutor.getByText("Because air")).toBeVisible();
        await page.keyboard.press("Escape");
        await expect(page.getByText("Slide 1 of 3")).toBeVisible();
        await expect(
          page.getByRole("button", { name: "Play", exact: true }),
        ).toBeVisible();
      });

      test("zero credits and offline keep the draft, show the reset and top-up, and never resend", async ({
        page,
        context,
        request,
      }) => {
        const posthog = await recordPosthog(page);
        const state = await mockTutor(page, "zero_credits");
        await seedLesson(page, request);
        await activate(page, mobile, "Ask tutor");
        const tutor = page
          .getByRole("region", { name: "Ask tutor" })
          .or(page.getByRole("dialog", { name: "Ask tutor" }));
        const input = tutor.getByRole("textbox", { name: "Message" });
        await input.fill("Explain parallax");
        await expect(tutor.getByText(/up to 5 credits/i)).toBeVisible();
        await input.press("Enter");
        const alert = tutor.getByRole("alert");
        await expect(alert).toContainText("Not enough credits");
        await expect(alert).toContainText("Next daily free credits");
        await expect(input).toHaveValue("Explain parallax");
        const topUp = alert.getByRole("link", { name: "See credit options" });
        await expect(topUp).toBeVisible();
        await page.waitForTimeout(800);
        expect(state.sent).toHaveLength(1);
        // Offline: the draft survives and Send is unavailable.
        await context.setOffline(true);
        await expect(tutor.getByText("Offline", { exact: true })).toBeVisible();
        await expect(input).toHaveValue("Explain parallax");
        await expect(
          tutor.getByRole("button", { name: "Send unavailable offline" }),
        ).toBeDisabled();
        await context.setOffline(false);
        await page.waitForTimeout(800);
        expect(state.sent).toHaveLength(1);
        await expect(input).toHaveValue("Explain parallax");
        // Recovery is measured without learner text.
        await topUp.evaluate((a) =>
          a.addEventListener("click", (e) => e.preventDefault()),
        );
        await topUp.click();
        await expect
          .poll(() => posthog.some((e) => e.event === "ui_recovery_selected"))
          .toBe(true);
        const recovery = posthog.find(
          (e) => e.event === "ui_recovery_selected",
        )!;
        expect(recovery.properties).toMatchObject({
          surface: "lesson_tutor",
          action: "top_up",
        });
        expect(String(recovery.properties.distinct_id)).toMatch(uuid);
        expect(JSON.stringify(posthog)).not.toContain("Explain parallax");
        expect(await noOverflow(page)).toBe(true);
      });
    });
