import type { Page, Route } from "@playwright/test";
import { test, expect } from "./accounts-fixture";

const courseId = "6f1d2b9e-0c3a-4b7e-9d1a-2f4c8e5a7b01";
const sse = (...events: [string, unknown][]) =>
  events
    .map(([name, data]) => `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`)
    .join("");

type Sent = { body: Record<string, unknown>; at: number };
type Options = {
  thread?: {
    thread: { id: string } | null;
    messages: Record<string, unknown>[];
  };
  messages?: (
    call: number,
    route: Route,
    body: Record<string, unknown>,
  ) => void;
};
async function mockTutor(page: Page, options: Options = {}) {
  const state = {
    quotes: [] as Record<string, unknown>[],
    accepted: [] as string[],
    sent: [] as Sent[],
    deleted: [] as string[],
    order: [] as string[],
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
    route.fulfill({ json: options.thread ?? { thread: null, messages: [] } }),
  );
  await page.route(`**/v1/courses/${courseId}/tutor/quote`, (route) => {
    const body = route.request().postDataJSON() as Record<string, unknown>;
    const quote = {
      id: `quote-${state.quotes.length + 1}`,
      content_ceiling: body.class === "long" ? 8 : 5,
      pricing_version: "unapproved-test",
      daily_free_eligible: true,
      expires_at: expires(),
    };
    state.quotes.push({ ...body, ...quote });
    state.order.push("quote");
    return route.fulfill({ json: quote });
  });
  await page.route("**/v1/quotes/*/accept", (route) => {
    state.accepted.push(route.request().url().split("/").at(-2) ?? "");
    state.order.push("accept");
    return route.fulfill({ json: { accepted: true } });
  });
  await page.route(`**/v1/courses/${courseId}/tutor/messages`, (route) => {
    const body = route.request().postDataJSON() as Record<string, unknown>;
    state.sent.push({ body, at: state.order.length });
    state.order.push("send");
    if (options.messages)
      return options.messages(state.sent.length, route, body);
    return route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: sse(
        ["thread", { thread_id: "t1" }],
        ["delta", { text: "Polaris sits " }],
        ["delta", { text: "above the pole." }],
        ["done", { message_id: "m1", thread_id: "t1", replayed: false }],
      ),
    });
  });
  await page.route("**/v1/tutor/threads/*", (route) => {
    state.deleted.push(route.request().url().split("/").at(-1) ?? "");
    return route.fulfill({ status: 204, body: "" });
  });
  return state;
}
const noOverflow = (page: Page) =>
  page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);

for (const colorScheme of ["light", "dark"] as const)
  for (const mobile of [false, true])
    test.describe(`tutor chat ${mobile ? "mobile touch" : "desktop keyboard"} ${colorScheme}`, () => {
      test.use({ hasTouch: mobile });
      test(`quotes, accepts, streams and keeps keyboard flow`, async ({
        page,
      }) => {
        await page.setViewportSize(
          mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
        );
        await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
        const state = await mockTutor(page);
        await page.goto(`/course/${courseId}`);
        await page.getByRole("button", { name: "Ask the tutor" }).click();
        const panel = page.getByRole("complementary", { name: "Tutor chat" });
        await expect(panel).toBeVisible();
        await expect(
          panel.getByText("Ask anything about this course"),
        ).toBeVisible();
        await expect(
          panel.getByText(/Daily free 28 · Purchased 124/),
        ).toBeVisible();
        // Starters fill the composer and never send.
        await panel
          .getByRole("button", {
            name: "Explain the main idea of this course simply",
          })
          .click();
        const input = panel.getByRole("textbox", { name: "Message" });
        await expect(input).toHaveValue(
          "Explain the main idea of this course simply",
        );
        expect(state.sent).toHaveLength(0);
        await panel.getByRole("radio", { name: /Long reply/ }).check();
        await expect(panel.getByText(/up to 8 credits/i)).toBeVisible();
        await input.press("Shift+Enter");
        await input.type("More");
        await expect(input).toHaveValue(
          "Explain the main idea of this course simply\nMore",
        );
        expect(state.sent).toHaveLength(0);
        await expect(panel.getByText(/up to 8 credits/i)).toBeVisible();
        await expect(panel.getByRole("log")).toHaveAttribute(
          "aria-live",
          "polite",
        );
        if (mobile) await panel.getByRole("button", { name: "Send" }).tap();
        else await input.press("Enter");
        await expect(
          panel.getByText("Polaris sits above the pole."),
        ).toBeVisible();
        await expect(
          panel.getByText("Explain the main idea of this course simply More"),
        ).toBeVisible();
        expect(state.sent).toHaveLength(1);
        const body = state.sent[0].body;
        expect(body).toMatchObject({
          class: "long",
          message: "Explain the main idea of this course simply\nMore",
          quote_id: expect.stringMatching(/^quote-/),
        });
        expect(body.idempotency_key).toMatch(/^[0-9a-f-]{36}$/);
        expect(state.accepted).toContain(body.quote_id);
        expect(state.order.lastIndexOf("accept")).toBeLessThan(
          state.order.lastIndexOf("send"),
        );
        await expect(input).toHaveValue("");
        await expect(input).toBeFocused();
        expect(await noOverflow(page)).toBe(true);
        if (mobile)
          await panel.getByRole("button", { name: "Close tutor chat" }).tap();
        else await page.keyboard.press("Escape");
        await expect(panel).toBeHidden();
        await expect(
          page.getByRole("button", { name: "Ask the tutor" }),
        ).toBeFocused();
      });
    });

test("insufficient credits keeps the draft and shows the upsell", async ({
  page,
}) => {
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  const state = await mockTutor(page, {
    messages: (_call, route) =>
      route.fulfill({
        status: 402,
        json: {
          error: "insufficient_credits",
          next_grant_at: "2030-01-02T00:00:00Z",
        },
      }),
  });
  await page.goto(`/course/${courseId}`);
  await page.getByRole("button", { name: "Ask the tutor" }).click();
  const input = page.getByRole("textbox", { name: "Message" });
  await input.fill("How far away is Polaris?");
  await expect(page.getByText(/up to 5 credits/i)).toBeVisible();
  await page.getByRole("button", { name: "Send", exact: true }).click();
  const alert = page
    .getByRole("alert")
    .filter({ hasText: "Not enough credits" });
  await expect(alert).toBeVisible();
  await expect(alert).toContainText("Next daily free credits");
  await expect(
    page.getByRole("link", { name: "See credit options" }),
  ).toHaveAttribute("href", "/credits");
  await expect(input).toHaveValue("How far away is Polaris?");
  await expect(page.getByRole("log").getByText("How far away")).toHaveCount(0);
  expect(state.sent).toHaveLength(1);
});

test("interrupted reply keeps its prefix and retries with a new key and quote", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const state = await mockTutor(page, {
    messages: (call, route) =>
      route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body:
          call === 1
            ? sse(
                ["thread", { thread_id: "t1" }],
                ["delta", { text: "Earth turns and" }],
                [
                  "error",
                  {
                    code: "engine_failed",
                    interrupted: true,
                    message_id: "m1",
                  },
                ],
              )
            : sse(
                ["thread", { thread_id: "t1" }],
                ["delta", { text: "A complete answer." }],
                [
                  "done",
                  { message_id: "m2", thread_id: "t1", replayed: false },
                ],
              ),
      }),
  });
  await page.goto(`/course/${courseId}`);
  await page.getByRole("button", { name: "Ask the tutor" }).click();
  const input = page.getByRole("textbox", { name: "Message" });
  await input.fill("Why do stars move?");
  await expect(page.getByText(/up to 5 credits/i)).toBeVisible();
  await input.press("Enter");
  await expect(page.getByText("Earth turns and")).toBeVisible();
  await expect(page.getByText("Reply interrupted")).toBeVisible();
  await page.getByRole("button", { name: /Ask again/ }).click();
  await expect(page.getByText("A complete answer.")).toBeVisible();
  expect(state.sent).toHaveLength(2);
  expect(state.sent[1].body.idempotency_key).not.toBe(
    state.sent[0].body.idempotency_key,
  );
  expect(state.sent[1].body.quote_id).not.toBe(state.sent[0].body.quote_id);
  expect(state.sent[1].body.message).toBe("Why do stars move?");
});

test("failed reply keeps the draft, retry works and offline disables sending", async ({
  page,
  context,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const state = await mockTutor(page, {
    messages: (call, route) =>
      call === 1
        ? route.fulfill({ status: 502, json: { error: "engine_unavailable" } })
        : route.fulfill({
            status: 200,
            contentType: "text/event-stream",
            body: sse(
              ["thread", { thread_id: "t1" }],
              ["delta", { text: "Recovered." }],
              ["done", { message_id: "m1", thread_id: "t1", replayed: false }],
            ),
          }),
  });
  await page.goto(`/course/${courseId}`);
  await page.getByRole("button", { name: "Ask the tutor" }).click();
  const input = page.getByRole("textbox", { name: "Message" });
  await input.fill("Tell me about Mars");
  await expect(page.getByText(/up to 5 credits/i)).toBeVisible();
  await input.press("Enter");
  const failed = page.getByRole("alert").filter({ hasText: "Couldn't reply" });
  await expect(failed).toContainText("Nothing was charged");
  await expect(input).toHaveValue("Tell me about Mars");
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(page.getByText("Recovered.")).toBeVisible();
  expect(state.sent).toHaveLength(2);
  await input.fill("Offline draft");
  await context.setOffline(true);
  await expect(page.getByText(/Tutor chat needs a connection/)).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Send unavailable offline" }),
  ).toBeDisabled();
  await expect(input).toHaveValue("Offline draft");
  await context.setOffline(false);
});

test("restores a saved thread and deletes it after confirming", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const long = "Long sentence without spaces ".repeat(40) + "x".repeat(200);
  const state = await mockTutor(page, {
    thread: {
      thread: { id: "t9" },
      messages: [
        {
          id: "a",
          role: "user",
          content: "Hi tutor",
          status: "complete",
          created_at: "2030-01-01T00:00:00Z",
        },
        {
          id: "b",
          role: "assistant",
          content: long,
          status: "complete",
          created_at: "2030-01-01T00:00:01Z",
        },
        {
          id: "c",
          role: "user",
          content: "Again",
          status: "complete",
          created_at: "2030-01-01T00:00:02Z",
        },
        {
          id: "d",
          role: "assistant",
          content: "Half an ans",
          status: "interrupted",
          created_at: "2030-01-01T00:00:03Z",
        },
      ],
    },
  });
  await page.goto(`/course/${courseId}`);
  await page.getByRole("button", { name: "Ask the tutor" }).click();
  await expect(page.getByText("Hi tutor")).toBeVisible();
  await expect(page.getByText("Half an ans")).toBeVisible();
  await expect(page.getByText("Reply interrupted")).toBeVisible();
  expect(await noOverflow(page)).toBe(true);
  await page.getByRole("button", { name: "Delete chat" }).click();
  const dialog = page.getByRole("dialog", { name: "Delete this chat?" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page.getByText("Hi tutor")).toHaveCount(0);
  expect(state.deleted).toEqual(["t9"]);
  await expect(page.getByText("Ask anything about this course")).toBeVisible();
});
