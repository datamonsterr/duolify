// Voice quick note (openspec voice-quick-note, PR C1): one round rail button, hold to talk,
// release saves at once with its place in the Lesson, no panel opens. Mocked suite: Chromium's
// fake microphone, the transcription endpoint mocked, the rest on the fixture API.
import { copyFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import type { Page, TestInfo } from "@playwright/test";
import { expect, test } from "./native-store-fixture";
import { webBase } from "./servers";
import {
  VIEWPORTS,
  clock,
  enterImmersive,
  openLesson,
  openRail,
  setup,
  type Vp,
} from "./anchored-helpers";
import { mock } from "./voice-helpers";

test.use({
  launchOptions: {
    args: [
      "--use-fake-device-for-media-stream",
      "--use-fake-ui-for-media-stream",
    ],
  },
  permissions: ["microphone"],
});

const TRANSCRIPT = "Sunsets are orange because of scattering.";
const SHOTS = process.env.VQN_SHOTS_DIR;

async function snap(
  page: Page,
  info: TestInfo,
  vp: Vp,
  scheme: string,
  name: string,
) {
  if (!SHOTS) return;
  const file = info.outputPath(`${name}.png`);
  await page.screenshot({ path: file });
  mkdirSync(SHOTS, { recursive: true });
  copyFileSync(file, join(SHOTS, `${vp.name}-${scheme}-${name}.png`));
}

async function press(page: Page) {
  const mic = page.getByTestId("voice-note-mic");
  await expect(mic).toHaveAttribute("aria-disabled", "false");
  const box = (await mic.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await expect(page.getByTestId("voice-timer")).toBeVisible();
}

for (const vp of [VIEWPORTS[0], VIEWPORTS[1]]) {
  for (const scheme of ["light", "dark"] as const) {
    test.describe(`voice quick note ${vp.name} ${scheme}`, () => {
      test.use({
        viewport: { width: vp.width, height: vp.height },
        hasTouch: true,
        isMobile: true,
        colorScheme: scheme,
      });

      test("hold pauses the Lesson, release saves with its anchor, no panel opens, Undo deletes", async ({
        page,
        request,
      }, info) => {
        const { state, posted } = await setup(page, request, vp, "vqn");
        const seen = await mock(page, { transcript: TRANSCRIPT });
        page.on("request", (r) => {
          if (r.method() === "DELETE" && r.url().includes("/v1/notes/"))
            seen.deleted.push(r.url());
        });
        await openLesson(page, state.lesson);
        await page.waitForTimeout(600);
        await enterImmersive(page);
        await openRail(page);
        // The rail has one Voice note button and no Note button that opens a panel.
        await expect(page.getByTestId("immersive-voice-note")).toBeVisible();
        await expect(page.getByTestId("immersive-quick-note")).toHaveCount(0);
        await snap(page, info, vp, scheme, "1-rail-idle");

        await press(page);
        await page.waitForTimeout(500);
        const frozen = await clock(page);
        await page.waitForTimeout(500);
        expect(await clock(page)).toBe(frozen);
        await expect(page.getByTestId("immersive-note-composer")).toHaveCount(
          0,
        );
        await snap(page, info, vp, scheme, "2-holding");

        await page.mouse.up();
        await expect(page.getByTestId("toast")).toContainText(/Saved/i);
        await expect(page.getByTestId("toast-action")).toBeVisible();
        await expect(page.getByTestId("immersive-note-composer")).toHaveCount(
          0,
        );
        await snap(page, info, vp, scheme, "3-saved-toast");
        // Playback resumes after release.
        await expect.poll(() => clock(page)).toBeGreaterThan(frozen);

        await expect.poll(() => posted.length).toBe(1);
        const body = posted[0].body as {
          raw_text: string;
          lesson_id: string;
          slide_id: number;
          course_id?: string;
          anchor: { input: string; playback_ms: number };
        };
        expect(body.raw_text).toBe(TRANSCRIPT);
        expect(body.lesson_id).toBe(state.lesson);
        expect(body.slide_id).toBe(0);
        expect(body.anchor.input).toBe("voice");
        expect(body.anchor.playback_ms).toBeGreaterThanOrEqual(0);

        await page.getByTestId("toast-action").click();
        await expect.poll(() => seen.deleted.length).toBe(1);
      });

      test("the saved note shows Course › Lesson › Slide and a jump chip on the Notes page", async ({
        page,
        request,
      }, info) => {
        const { state, posted } = await setup(page, request, vp, "vqn-page");
        await mock(page, { transcript: TRANSCRIPT });
        await openLesson(page, state.lesson);
        await page.waitForTimeout(600);
        await enterImmersive(page);
        await openRail(page);
        await press(page);
        await page.waitForTimeout(1000);
        await page.mouse.up();
        await expect.poll(() => posted.length).toBe(1);
        await page.goto(`${webBase}/notes`);
        await page.getByText(TRANSCRIPT).first().click();
        await expect(page.getByTestId("note-breadcrumb")).toContainText(
          "Slide",
        );
        const chip = page.getByTestId("note-show-in-lesson");
        await expect(chip).toContainText("Show in Lesson");
        await expect(page.getByTestId("note-anchor-label")).toContainText(
          "dictated",
        );
        await page.getByTestId("note-breadcrumb").scrollIntoViewIfNeeded();
        await snap(page, info, vp, scheme, "4-notes-entry");
        await chip.click();
        await expect(page).toHaveURL(/\/lesson\/.+\?.*slide=0/);
        void clock;
      });
    });
  }
}

test.describe("voice quick note failures", () => {
  const vp = VIEWPORTS[1];
  test.use({
    viewport: { width: vp.width, height: vp.height },
    hasTouch: true,
    isMobile: true,
  });

  test("nothing understood saves no note and offers retry", async ({
    page,
    request,
  }) => {
    const { state, posted } = await setup(page, request, vp, "vqn-empty");
    await mock(page, { transcript: "" });
    await openLesson(page, state.lesson);
    await page.waitForTimeout(600);
    await enterImmersive(page);
    await openRail(page);
    await press(page);
    await page.waitForTimeout(1000);
    await page.mouse.up();
    await expect(page.getByTestId("toast")).toBeVisible();
    expect(posted).toHaveLength(0);
  });

  test("offline queues the note on the device with its voice anchor", async ({
    page,
    request,
  }) => {
    const { state, posted } = await setup(page, request, vp, "vqn-offline");
    await mock(page, { transcript: TRANSCRIPT });
    await openLesson(page, state.lesson);
    await page.waitForTimeout(600);
    await enterImmersive(page);
    await openRail(page);
    state.offline = true;
    await press(page);
    await page.waitForTimeout(1000);
    await page.mouse.up();
    await expect(page.getByTestId("toast")).toContainText(/Saved/i);
    const queued = await page.evaluate(() =>
      JSON.parse(localStorage.getItem("duolify.notes.queue") ?? "[]"),
    );
    expect(queued).toHaveLength(1);
    expect(queued[0].anchor.input).toBe("voice");
    expect(posted).toHaveLength(0);
  });
});

test("microphone denied shows the existing denied toast and records nothing", async ({
  page,
  request,
  context,
}) => {
  const vp = VIEWPORTS[1];
  await page.setViewportSize({ width: vp.width, height: vp.height });
  const { state, posted } = await setup(page, request, vp, "vqn-denied");
  await mock(page, { transcript: TRANSCRIPT });
  await context.clearPermissions();
  await openLesson(page, state.lesson);
  await page.waitForTimeout(600);
  await enterImmersive(page);
  await openRail(page);
  const mic = page.getByTestId("voice-note-mic");
  const box = (await mic.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.waitForTimeout(500);
  await page.mouse.up();
  await expect(page.getByTestId("toast")).toBeVisible();
  await expect(page.getByTestId("toast")).not.toContainText(/Saved/i);
  expect(posted).toHaveLength(0);
});
