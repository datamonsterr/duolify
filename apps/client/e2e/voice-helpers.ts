// Shared fixtures for the voice and overlay specs: mocked transcription and tutor endpoints.
import type { Page, Route } from "@playwright/test";
import { enterImmersive, openLesson, openRail } from "./anchored-helpers";
import { expect } from "./native-store-fixture";
import { apiBase } from "./servers";

export const QUESTION = "Why does the sky turn orange at sunset?";
export const REPLY = "Because the light is scattered by the air.";

export type Seen = {
  uploads: { type: string; text: string }[];
  messages: Record<string, unknown>[];
  deleted: string[];
};

export async function mock(
  page: Page,
  opts: {
    voice?: boolean;
    explained?: boolean;
    transcript?: string;
    status?: number;
    body?: object;
    language?: string;
  } = {},
): Promise<Seen> {
  const seen: Seen = { uploads: [], messages: [], deleted: [] };
  // The first-use explainer is skipped except in the test that covers it.
  if (opts.explained !== false)
    await page
      .context()
      .addInitScript(() =>
        localStorage.setItem("duolify.voice.explained", "1"),
      );
  await page.route("**/v1/pricing", async (route) => {
    const headers = route.request().headers();
    delete headers.origin;
    const response = await route.fetch({
      url: `${apiBase}/v1/pricing`,
      headers,
    });
    const json = await response.json();
    json.transcribe = opts.voice === false ? null : { daily_requests: 60 };
    await route.fulfill({ response, json });
  });
  await page.route("**/v1/voice/transcriptions", async (route: Route) => {
    const request = route.request();
    seen.uploads.push({
      type: request.headers()["content-type"] ?? "",
      text: request.postData() ? "multipart" : "",
    });
    await route.fulfill({
      status: opts.status ?? 200,
      json: opts.body ?? {
        text: opts.transcript ?? QUESTION,
        language: opts.language ?? "en",
        duration_ms: 1500,
        served_model: "qwen/qwen3-asr-0.6b",
        fallback_used: false,
      },
    });
  });
  await page.route("**/v1/me/credits", (route) =>
    route.fulfill({
      json: {
        available: 50,
        daily_free: {
          available: 30,
          granted: 30,
          resets_at: "2030-01-02T00:00:00Z",
          timezone: "UTC",
        },
        purchased: 20,
        subscription: 0,
        reserved: 0,
        recent_activity: [],
      },
    }),
  );
  await page.route("**/v1/courses/*/tutor/thread", (route) =>
    route.fulfill({ json: { thread: null, messages: [] } }),
  );
  await page.route("**/v1/courses/*/tutor/personas*", (route) =>
    route.fulfill({ status: 404, json: { error: "not_found" } }),
  );
  await page.route("**/v1/courses/*/tutor/quote", (route) =>
    route.fulfill({
      json: {
        id: "q1",
        content_ceiling: 2,
        pricing_version: "v1",
        daily_free_eligible: true,
        expires_at: new Date(Date.now() + 600_000).toISOString(),
      },
    }),
  );
  await page.route("**/v1/quotes/*/accept", (route) =>
    route.fulfill({ json: {} }),
  );
  await page.route("**/v1/courses/*/tutor/messages", async (route: Route) => {
    seen.messages.push(JSON.parse(route.request().postData() ?? "{}"));
    await route.fulfill({
      status: 200,
      headers: { "content-type": "text/event-stream" },
      body:
        `event: thread\ndata: {"thread_id":"22222222-2222-4222-8222-222222222222"}\n\n` +
        `event: delta\ndata: {"text":${JSON.stringify(REPLY)}}\n\n` +
        `event: done\ndata: {"message_id":"33333333-3333-4333-8333-333333333333","thread_id":"22222222-2222-4222-8222-222222222222","replayed":false}\n\n`,
    });
  });
  return seen;
}

export async function openPanel(page: Page, lesson: string) {
  await openLesson(page, lesson);
  await page.waitForTimeout(600);
  await enterImmersive(page);
  await openRail(page);
  await page.getByTestId("immersive-ask").click();
  await expect(page.getByTestId("answer-panel")).toBeVisible();
}
