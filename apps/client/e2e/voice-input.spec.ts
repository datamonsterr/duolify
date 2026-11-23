// Voice input (immersive-mobile-lesson-player group 6, client side): push-to-talk in the
// immersive answer panel and the Quick note composer. Mocked suite: Chromium's fake microphone
// supplies audio, the transcription endpoint and the tutor endpoints are fulfilled here, and the
// rest goes to the fixture API.
import type { Page, Route } from "@playwright/test";
import { expect, test } from "./native-store-fixture";
import {
  VIEWPORTS,
  enterImmersive,
  openLesson,
  openRail,
  setup,
  shot,
} from "./anchored-helpers";
import { apiBase } from "./servers";

const vp = VIEWPORTS[1];
const QUESTION = "Why does the sky turn orange at sunset?";
const REPLY = "Because the light is scattered by the air.";

test.use({
  viewport: { width: vp.width, height: vp.height },
  hasTouch: true,
  isMobile: true,
  launchOptions: {
    args: [
      "--use-fake-device-for-media-stream",
      "--use-fake-ui-for-media-stream",
    ],
  },
  permissions: ["microphone"],
});

type Seen = {
  uploads: { type: string; text: string }[];
  messages: Record<string, unknown>[];
  deleted: string[];
};

async function mock(
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

async function openPanel(page: Page, lesson: string) {
  await openLesson(page, lesson);
  await page.waitForTimeout(600);
  await enterImmersive(page);
  await openRail(page);
  await page.getByTestId("immersive-ask").click();
  await expect(page.getByTestId("answer-panel")).toBeVisible();
}

async function hold(page: Page, ms: number) {
  const mic = page.getByTestId("voice-mic");
  await expect(mic).toHaveAttribute("aria-disabled", "false");
  const box = (await mic.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await expect(page.getByTestId("voice-live")).toBeVisible();
  await page.waitForTimeout(ms);
  return box;
}

test("the auto-send option is a normal checkbox beside its label (design review)", async ({
  page,
  request,
}) => {
  const { state } = await setup(page, request, vp, "voice-auto-layout");
  await mock(page);
  await openPanel(page, state.lesson);
  const box = (await page.getByTestId("voice-auto-send").boundingBox())!;
  expect(box.width).toBeLessThanOrEqual(32);
  expect(box.height).toBeLessThanOrEqual(32);
  const label = (await page.locator("label.voice-auto").boundingBox())!;
  // Checkbox and text share a row: the label is one line tall, not a stacked column.
  expect(label.height).toBeLessThan(box.height * 2.5);
});

test("hold to talk, release: the transcript fills the composer and is sent after the 2 s window as a voice question", async ({
  page,
  request,
}, info) => {
  const { state } = await setup(page, request, vp, "voice-send");
  const seen = await mock(page);
  await openPanel(page, state.lesson);
  await shot(page, info, vp, "voice-1-idle");
  await hold(page, 900);
  // Level meter and timer are visible while recording.
  await expect(page.getByTestId("voice-timer")).toBeVisible();
  await shot(page, info, vp, "voice-2-recording");
  await expect(
    page.getByRole("meter", { name: "Microphone level" }),
  ).toBeVisible();
  await page.mouse.up();
  const field = page.getByRole("textbox", { name: "Your question" });
  await expect(field).toHaveValue(QUESTION);
  await expect(page.getByTestId("voice-countdown")).toBeVisible();
  await shot(page, info, vp, "voice-3-transcript-countdown");
  await expect(page.getByText(REPLY)).toBeVisible({ timeout: 8000 });
  expect(seen.uploads).toHaveLength(1);
  expect(seen.uploads[0].type).toContain("multipart/form-data");
  expect(seen.messages).toHaveLength(1);
  expect(seen.messages[0].message ?? seen.messages[0].text).toBeTruthy();
});

test("editing the transcript within the window cancels the automatic send", async ({
  page,
  request,
}) => {
  const { state } = await setup(page, request, vp, "voice-edit");
  const seen = await mock(page);
  await openPanel(page, state.lesson);
  await hold(page, 900);
  await page.mouse.up();
  const field = page.getByRole("textbox", { name: "Your question" });
  await expect(field).toHaveValue(QUESTION);
  await field.press("End");
  await field.pressSequentially(" Please explain.");
  await page.waitForTimeout(2600);
  expect(seen.messages).toHaveLength(0);
  await page.getByTestId("answer-send").click();
  await expect(page.getByText(REPLY)).toBeVisible();
  expect(seen.messages).toHaveLength(1);
});

test("Vietnamese transcripts are not sent automatically", async ({
  page,
  request,
}) => {
  const { state } = await setup(page, request, vp, "voice-vi");
  const seen = await mock(page, {
    transcript: "Tại sao bầu trời màu cam?",
    language: "vi",
  });
  await openPanel(page, state.lesson);
  await hold(page, 900);
  await page.mouse.up();
  await expect(
    page.getByRole("textbox", { name: "Your question" }),
  ).toHaveValue("Tại sao bầu trời màu cam?");
  await page.waitForTimeout(2800);
  expect(seen.messages).toHaveLength(0);
  await expect(page.getByTestId("voice-countdown")).toHaveCount(0);
});

test("sliding off the control cancels and uploads nothing", async ({
  page,
  request,
}) => {
  const { state } = await setup(page, request, vp, "voice-cancel");
  const seen = await mock(page);
  await openPanel(page, state.lesson);
  const box = await hold(page, 700);
  await page.mouse.move(box.x + box.width + 200, box.y - 100, { steps: 4 });
  await expect(page.getByTestId("voice-live")).toHaveCount(0);
  await page.mouse.up();
  await page.waitForTimeout(500);
  expect(seen.uploads).toHaveLength(0);
  await expect(
    page.getByRole("textbox", { name: "Your question" }),
  ).toHaveValue("");
});

test("Escape cancels a tap-to-toggle recording", async ({ page, request }) => {
  const { state } = await setup(page, request, vp, "voice-esc");
  const seen = await mock(page);
  await openPanel(page, state.lesson);
  const mic = page.getByTestId("voice-mic");
  await expect(mic).toHaveAttribute("aria-disabled", "false");
  await mic.click();
  await expect(page.getByTestId("voice-live")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByTestId("voice-live")).toHaveCount(0);
  expect(seen.uploads).toHaveLength(0);
});

test("tap to toggle: a second tap finishes the recording", async ({
  page,
  request,
}) => {
  const { state } = await setup(page, request, vp, "voice-tap");
  const seen = await mock(page);
  await openPanel(page, state.lesson);
  const mic = page.getByTestId("voice-mic");
  await expect(mic).toHaveAttribute("aria-disabled", "false");
  await mic.click();
  await expect(page.getByTestId("voice-live")).toBeVisible();
  await page.waitForTimeout(900);
  await mic.click();
  await expect(
    page.getByRole("textbox", { name: "Your question" }),
  ).toHaveValue(QUESTION);
  expect(seen.uploads).toHaveLength(1);
});

test("recording stops at 30 seconds and the audio is transcribed", async ({
  page,
  request,
}) => {
  test.setTimeout(90_000);
  const { state } = await setup(page, request, vp, "voice-30");
  const seen = await mock(page);
  await openPanel(page, state.lesson);
  await hold(page, 100);
  // Still holding at 31 s: the client has already stopped and uploaded at 30 s.
  await expect.poll(() => seen.uploads.length, { timeout: 40_000 }).toBe(1);
  await page.mouse.up();
  await expect(
    page.getByRole("textbox", { name: "Your question" }),
  ).toHaveValue(QUESTION);
});

test("first use explains the microphone before asking for it", async ({
  page,
  request,
}) => {
  const { state } = await setup(page, request, vp, "voice-explain");
  await mock(page, { explained: false });
  await openPanel(page, state.lesson);
  const mic = page.getByTestId("voice-mic");
  await expect(mic).toHaveAttribute("aria-disabled", "false");
  await mic.click();
  const explain = page.getByTestId("voice-explain");
  await expect(explain).toContainText("does not keep it (zero data retention)");
  await page.getByTestId("voice-explain-allow").click();
  await expect(explain).toHaveCount(0);
});

test("permission denied explains how to enable the microphone; typing still works", async ({
  page,
  request,
}) => {
  const { state } = await setup(page, request, vp, "voice-denied");
  await mock(page);
  await page.context().addInitScript(() => {
    navigator.mediaDevices.getUserMedia = () =>
      Promise.reject(new DOMException("denied", "NotAllowedError"));
  });
  await openPanel(page, state.lesson);
  const mic = page.getByTestId("voice-mic");
  await expect(mic).toHaveAttribute("aria-disabled", "false");
  await mic.click();
  await expect(page.getByTestId("voice-notice")).toContainText(
    "Microphone access is off",
  );
  const field = page.getByRole("textbox", { name: "Your question" });
  await field.fill("typed instead");
  await expect(page.getByTestId("answer-send")).toBeEnabled();
});

test("offline: the control explains that voice needs a connection", async ({
  page,
  request,
  context,
}) => {
  const { state } = await setup(page, request, vp, "voice-offline");
  await mock(page);
  await openPanel(page, state.lesson);
  await expect(page.getByTestId("voice-mic")).toHaveAttribute(
    "aria-disabled",
    "false",
  );
  await context.setOffline(true);
  await expect(page.getByTestId("voice-blocked")).toContainText(
    "needs a connection",
  );
  await expect(page.getByTestId("voice-mic")).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  await context.setOffline(false);
});

test("daily cap: the control says when it resets and typing remains", async ({
  page,
  request,
}) => {
  const { state } = await setup(page, request, vp, "voice-cap");
  await mock(page, {
    status: 429,
    body: {
      error: "voice_daily_cap",
      code: "voice_daily_cap",
      resets_at: new Date(Date.now() + 3 * 3600_000).toISOString(),
    },
  });
  await openPanel(page, state.lesson);
  await hold(page, 800);
  await page.mouse.up();
  await expect(page.getByTestId("voice-blocked")).toContainText(
    "today's voice limit",
  );
  await expect(page.getByTestId("voice-blocked")).toContainText("resets at");
  await page
    .getByRole("textbox", { name: "Your question" })
    .fill("still typing");
  await expect(page.getByTestId("answer-send")).toBeEnabled();
});

test("voice is unavailable without a transcription rate", async ({
  page,
  request,
}) => {
  const { state } = await setup(page, request, vp, "voice-unavailable");
  const seen = await mock(page, { voice: false });
  await openPanel(page, state.lesson);
  await expect(page.getByTestId("voice-blocked")).toContainText(
    "not available yet",
  );
  await expect(page.getByTestId("voice-mic")).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  expect(seen.uploads).toHaveLength(0);
});

test("a dictated Quick note is saved at once with a voice anchor, and Undo removes it", async ({
  page,
  request,
}) => {
  const { state, posted } = await setup(page, request, vp, "voice-note");
  const seen = await mock(page, {
    transcript: "Sunsets are orange because of scattering.",
  });
  page.on("request", (r) => {
    if (r.method() === "DELETE" && r.url().includes("/v1/notes/"))
      seen.deleted.push(r.url());
  });
  await openLesson(page, state.lesson);
  await page.waitForTimeout(600);
  await enterImmersive(page);
  await openRail(page);
  await page.getByTestId("immersive-quick-note").click();
  await expect(page.getByTestId("immersive-note-composer")).toBeVisible();
  await hold(page, 900);
  await page.mouse.up();
  await expect(page.getByTestId("voice-note-saved")).toBeVisible();
  await expect.poll(() => posted.length).toBe(1);
  const body = posted[0].body as {
    raw_text: string;
    anchor: { input: string };
  };
  expect(body.raw_text).toBe("Sunsets are orange because of scattering.");
  expect(body.anchor.input).toBe("voice");
  await page.getByTestId("voice-note-undo").click();
  await expect.poll(() => seen.deleted.length).toBe(1);
  await expect(page.getByTestId("voice-note-saved")).toHaveCount(0);
});
