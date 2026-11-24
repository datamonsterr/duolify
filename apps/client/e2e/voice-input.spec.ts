// Voice input (immersive-mobile-lesson-player group 6, client side): push-to-talk in the
// immersive answer panel and the Quick note composer. Mocked suite: Chromium's fake microphone
// supplies audio, the transcription endpoint and the tutor endpoints are fulfilled here, and the
// rest goes to the fixture API.
import type { Page } from "@playwright/test";
import { expect, test } from "./native-store-fixture";
import {
  VIEWPORTS,
  enterImmersive,
  openLesson,
  openRail,
  setup,
  shot,
} from "./anchored-helpers";
import { REPLY, mock, openPanel, QUESTION } from "./voice-helpers";

const vp = VIEWPORTS[1];

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

async function hold(page: Page, ms: number) {
  const mic = page.getByTestId("voice-mic");
  await expect(mic).toHaveAttribute("aria-disabled", "false");
  const box = (await mic.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await expect(page.getByTestId("voice-timer")).toBeVisible();
  await page.waitForTimeout(ms);
  return box;
}

test("Talk is a small round bubble with no auto-send checkbox in the panel", async ({
  page,
  request,
}) => {
  const { state } = await setup(page, request, vp, "voice-bubble");
  await mock(page);
  await openPanel(page, state.lesson);
  const box = (await page.getByTestId("voice-mic").boundingBox())!;
  expect(box.width).toBeGreaterThanOrEqual(44);
  expect(box.width).toBeLessThanOrEqual(48);
  expect(box.height).toBeLessThanOrEqual(48);
  await expect(page.getByTestId("voice-auto-send")).toHaveCount(0);
  await expect(page.getByRole("heading")).toHaveCount(0);
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
  await expect(page.getByTestId("voice-timer")).toHaveCount(0);
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
  await expect(page.getByTestId("voice-timer")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByTestId("voice-timer")).toHaveCount(0);
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
  await expect(page.getByTestId("voice-timer")).toBeVisible();
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
  const toast = page.getByTestId("toast");
  await expect(toast).toContainText("Audio is not saved");
  await page.getByTestId("toast-action").click();
  await expect(toast).toHaveCount(0);
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
  await expect(page.getByTestId("toast")).toContainText(
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
  await expect(page.getByTestId("toast")).toContainText("needs a connection");
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
  await expect(page.getByTestId("toast")).toContainText("today's voice limit");
  await expect(page.getByTestId("toast")).toContainText("resets at");
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
  await page.getByTestId("voice-mic").click({ force: true });
  await expect(page.getByTestId("toast")).toContainText("not available yet");
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
  await expect(page.getByTestId("toast")).toBeVisible();
  await expect.poll(() => posted.length).toBe(1);
  const body = posted[0].body as {
    raw_text: string;
    anchor: { input: string };
  };
  expect(body.raw_text).toBe("Sunsets are orange because of scattering.");
  expect(body.anchor.input).toBe("voice");
  await page.getByTestId("toast-action").click();
  await expect.poll(() => seen.deleted.length).toBe(1);
  await expect(page.getByTestId("toast")).toHaveCount(0);
});

test.describe("plain voice hints at phone size", () => {
  const phone = VIEWPORTS[0];
  test.use({ viewport: { width: phone.width, height: phone.height } });
  test("permission denied shows a short toast beside the mic", async ({
    page,
    request,
  }, info) => {
    const { state } = await setup(page, request, phone, "voice-denied-phone");
    await mock(page);
    await page.context().addInitScript(() => {
      navigator.mediaDevices.getUserMedia = () =>
        Promise.reject(new DOMException("denied", "NotAllowedError"));
    });
    await openPanel(page, state.lesson);
    await page.getByTestId("voice-mic").click();
    await expect(page.getByTestId("toast")).toContainText(
      "Microphone access is off",
    );
    await shot(page, info, phone, "voice-denied-toast");
  });
});
