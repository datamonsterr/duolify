import type { Page } from "@playwright/test";
import { test, expect } from "./accounts-fixture";

const courseId = "6f1d2b9e-0c3a-4b7e-9d1a-2f4c8e5a7b02";
const sse = (...events: [string, unknown][]) =>
  events
    .map(([name, data]) => `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`)
    .join("");

declare global {
  interface Window {
    tutorStream: {
      push: (frame: string) => void;
      close: () => void;
      fail: () => void;
      aborted: boolean;
      opened: number;
      body: { idempotency_key?: string };
    };
  }
}

/**
 * Holds the tutor SSE response open so a test decides when deltas, errors or a dropped
 * connection happen. Aborting the request errors the stream like a real fetch.
 */
async function holdStream(page: Page) {
  await page.addInitScript(() => {
    const real = window.fetch.bind(window);
    const encoder = new TextEncoder();
    let controller: ReadableStreamDefaultController<Uint8Array> | undefined;
    const handle = {
      aborted: false,
      opened: 0,
      body: {} as { idempotency_key?: string },
      push: (frame: string) => controller?.enqueue(encoder.encode(frame)),
      close: () => controller?.close(),
      fail: () => controller?.error(new TypeError("network error")),
    };
    window.tutorStream = handle;
    window.fetch = (input, init) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (!url.endsWith("/tutor/messages") || init?.method !== "POST")
        return real(input, init);
      // Only the first send is held; later sends reach the mocked route.
      if (handle.opened >= 1) return real(input, init);
      handle.opened++;
      handle.body = JSON.parse(String(init?.body ?? "{}"));
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
const openStream = (page: Page) =>
  page.evaluate(() =>
    (window.tutorStream as unknown as { open: () => void }).open(),
  );
const push = (page: Page, frame: string) =>
  page.evaluate((f) => window.tutorStream.push(f), frame);

type Calls = {
  sent: Record<string, unknown>[];
  stops: { key: string; at: number }[];
  turns: string[];
  order: string[];
  stopStatus: number;
};
async function mockTutor(
  page: Page,
  turn: (key: string) => Record<string, unknown> | undefined = () => undefined,
) {
  const state: Calls = {
    sent: [],
    stops: [],
    turns: [],
    order: [],
    stopStatus: 200,
  };
  const expires = () => new Date(Date.now() + 600000).toISOString();
  await page.route(`**/v1/courses/${courseId}`, (route) =>
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
  await page.route(`**/v1/courses/${courseId}/tutor/thread`, (route) =>
    route.fulfill({ json: { thread: null, messages: [] } }),
  );
  let quotes = 0;
  await page.route(`**/v1/courses/${courseId}/tutor/quote`, (route) =>
    route.fulfill({
      json: {
        id: `quote-${++quotes}`,
        content_ceiling: 5,
        pricing_version: "unapproved-test",
        daily_free_eligible: true,
        expires_at: expires(),
      },
    }),
  );
  await page.route("**/v1/quotes/*/accept", (route) => {
    state.order.push("accept");
    return route.fulfill({ json: { accepted: true } });
  });
  await page.route(`**/v1/courses/${courseId}/tutor/turns/**`, (route) => {
    const url = new URL(route.request().url());
    const parts = url.pathname.split("/");
    if (route.request().method() === "POST" && parts.at(-1) === "stop") {
      state.stops.push({
        key: decodeURIComponent(parts.at(-2) ?? ""),
        at: state.order.length,
      });
      state.order.push("stop");
      if (state.stopStatus !== 200)
        return route.fulfill({
          status: state.stopStatus,
          json: { error: "not_found" },
        });
      return route.fulfill({
        json: {
          state: "stopped",
          status: "stopped",
          stopped: true,
          message_id: "m-stopped",
        },
      });
    }
    const key = decodeURIComponent(parts.at(-1) ?? "");
    state.turns.push(key);
    const found = turn(key);
    return found
      ? route.fulfill({ json: found })
      : route.fulfill({ status: 404, json: { error: "not_found" } });
  });
  await page.route(
    `**/v1/courses/${courseId}/tutor/messages`,
    async (route) => {
      state.sent.push(
        route.request().postDataJSON() as Record<string, unknown>,
      );
      state.order.push("send");
      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: sse(
          ["thread", { thread_id: "t1" }],
          ["delta", { text: "Recovered answer." }],
          ["done", { message_id: "m-done", thread_id: "t1", replayed: false }],
        ),
      });
    },
  );
  return state;
}
const noOverflow = (page: Page) =>
  page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);

async function ask(page: Page, text: string) {
  await page.getByRole("button", { name: "Ask the tutor" }).click();
  const input = page.getByRole("textbox", { name: "Message" });
  await input.fill(text);
  await expect(page.getByText(/up to 5 credits/i)).toBeVisible();
  await input.press("Enter");
  return input;
}

for (const colorScheme of ["light", "dark"] as const)
  for (const mobile of [false, true])
    test.describe(`stop mid-reply ${mobile ? "mobile touch" : "desktop keyboard"} ${colorScheme}`, () => {
      test.use({ hasTouch: mobile });
      test("Stop asks the server first, keeps the partial reply and frees the composer", async ({
        page,
      }) => {
        await page.setViewportSize(
          mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
        );
        await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
        await holdStream(page);
        const state = await mockTutor(page);
        await page.goto(`/course/${courseId}`);
        const input = await ask(page, "Why do stars move?");
        await openStream(page);
        await push(
          page,
          sse(
            ["thread", { thread_id: "t1" }],
            ["delta", { text: "Earth turns and" }],
          ),
        );
        const panel = page.getByRole("complementary", { name: "Tutor chat" });
        await expect(panel.getByText("Earth turns and")).toBeVisible();
        const stop = panel.getByRole("button", { name: "Stop reply" });
        await expect(stop).toBeEnabled();
        // No focus theft while streaming; reduced motion removes the writing chip animation.
        await expect(panel.getByRole("log")).toHaveAttribute(
          "aria-busy",
          "true",
        );
        const animation = await panel
          .locator(".tutor-chip")
          .evaluate((n) => getComputedStyle(n).animationName);
        expect(animation).toBe("none");
        if (mobile) await stop.tap();
        else {
          await stop.focus();
          await page.keyboard.press("Enter");
        }
        const banner = panel.getByRole("status").filter({ hasText: "Stopped" });
        await expect(banner).toBeVisible();
        await expect(banner).toContainText("You stopped the reply");
        await expect(panel.getByText("Earth turns and")).toBeVisible();
        expect(state.stops).toHaveLength(1);
        const sentKey = await page.evaluate(
          () => window.tutorStream.body.idempotency_key,
        );
        expect(state.stops[0].key).toBe(sentKey);
        expect(await page.evaluate(() => window.tutorStream.aborted)).toBe(
          true,
        );
        // The server stop is issued before the connection is aborted.
        expect(state.order.at(-1)).toBe("stop");
        await expect(input).toBeEnabled();
        await expect(input).toBeFocused();
        await expect(
          panel.getByRole("button", { name: "Send", exact: true }),
        ).toBeDisabled();
        expect(await noOverflow(page)).toBe(true);
      });
    });

test("Stop is reachable before the first token and keeps the draft", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await holdStream(page);
  const state = await mockTutor(page);
  state.stopStatus = 404; // the turn never produced a delta: nothing stored
  await page.goto(`/course/${courseId}`);
  const input = await ask(page, "Tell me about Mars");
  const stop = page.getByRole("button", { name: "Stop reply" });
  await expect(stop).toBeEnabled();
  await stop.click();
  await expect(
    page.getByRole("status").filter({ hasText: "Stopped" }),
  ).toBeVisible();
  await expect(input).toHaveValue("Tell me about Mars");
  await expect(
    page.getByRole("log").getByText("Tell me about Mars"),
  ).toHaveCount(0);
  expect(state.stops).toHaveLength(1);
  expect(state.stops[0].key).toBe(
    await page.evaluate(() => window.tutorStream.body.idempotency_key),
  );
  expect(await page.evaluate(() => window.tutorStream.aborted)).toBe(true);
});

test("failing before the first token keeps the draft, says nothing was charged and retries", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const state = await mockTutor(page);
  let call = 0;
  await page.route(`**/v1/courses/${courseId}/tutor/messages`, (route) => {
    state.sent.push(route.request().postDataJSON() as Record<string, unknown>);
    call++;
    return call === 1
      ? route.fulfill({
          status: 502,
          json: { error: "first_token_timeout", retryable: true },
        })
      : route.fulfill({
          status: 200,
          contentType: "text/event-stream",
          body: sse(
            ["thread", { thread_id: "t1" }],
            ["delta", { text: "Recovered answer." }],
            [
              "done",
              { message_id: "m-done", thread_id: "t1", replayed: false },
            ],
          ),
        });
  });
  await page.goto(`/course/${courseId}`);
  const input = await ask(page, "Tell me about Mars");
  const failed = page.getByRole("alert").filter({ hasText: "Couldn't reply" });
  await expect(failed).toContainText("Nothing was charged");
  await expect(input).toHaveValue("Tell me about Mars");
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(page.getByText("Recovered answer.")).toBeVisible();
  expect(state.sent).toHaveLength(2);
});

test("an idle timeout after the first token keeps the prefix and offers Ask again with a new key", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await holdStream(page);
  const state = await mockTutor(page);
  await page.goto(`/course/${courseId}`);
  await ask(page, "Why do stars move?");
  await openStream(page);
  await push(
    page,
    sse(
      ["thread", { thread_id: "t1" }],
      ["delta", { text: "Earth turns and" }],
      [
        "error",
        {
          code: "idle_timeout",
          interrupted: true,
          retryable: true,
          message_id: "m-idle",
        },
      ],
    ),
  );
  await expect(page.getByText("Earth turns and")).toBeVisible();
  const note = page.getByText("The tutor stopped responding");
  await expect(note).toBeVisible();
  const again = page.getByRole("button", { name: /Ask again/ });
  await expect(again).toBeEnabled();
  // The partial is stored, so the client does not ask the server to stop anything.
  expect(state.stops).toHaveLength(0);
  await page.evaluate(() => {
    window.tutorStream.aborted = false;
  });
  await again.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByText("Recovered answer.")).toBeVisible();
  expect(state.sent).toHaveLength(1); // the held first send never reached the route
  expect(state.sent[0].idempotency_key).not.toBe(
    await page.evaluate(() => window.tutorStream.body.idempotency_key),
  );
  expect(state.sent[0].quote_id).not.toBe("quote-1");
});

test("a dropped connection mid-reply recovers the stored partial from the server", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await holdStream(page);
  let key = "";
  const state = await mockTutor(page, (asked) => {
    key = asked;
    return {
      state: "interrupted",
      end_reason: "disconnect",
      thread_id: "t1",
      message: {
        id: "m-stored",
        content: "Earth turns and spins",
        status: "interrupted",
      },
    };
  });
  await page.goto(`/course/${courseId}`);
  await ask(page, "Why do stars move?");
  await openStream(page);
  await push(
    page,
    sse(
      ["thread", { thread_id: "t1" }],
      ["delta", { text: "Earth turns and" }],
    ),
  );
  await expect(page.getByText("Earth turns and")).toBeVisible();
  await page.evaluate(() => window.tutorStream.fail());
  // The stored text wins over what arrived locally, and the retry option appears.
  await expect(page.getByText("Earth turns and spins")).toBeVisible();
  await expect(page.getByText("Reply interrupted")).toBeVisible();
  await expect(page.getByRole("button", { name: /Ask again/ })).toBeEnabled();
  expect(state.turns).toHaveLength(1);
  expect(key).toBe(
    await page.evaluate(() => window.tutorStream.body.idempotency_key),
  );
  expect(state.stops).toHaveLength(0);
});
