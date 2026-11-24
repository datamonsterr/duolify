// Voice note formatting (openspec voice-quick-note, PR C2): free formats badge on the rail icon,
// the "Format" toast action past the free turns, and the formatted Notes entry with Show original.
// Mocked suite: the formatting state comes from route mocks, the rest from the fixture API.
import { copyFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import type { Page, TestInfo } from "@playwright/test";
import { expect, test } from "./native-store-fixture";
import { webBase } from "./servers";
import {
  VIEWPORTS,
  enterImmersive,
  expectToastClearOfTitle,
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

const RAW = "an unbuffered chanel blocks the sender until a receiver is reddy";
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

const json = (body: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify(body),
});

/** The API says how many free formats are left; everything else falls through to the fixture API. */
async function freeLeft(page: Page, left: () => number) {
  await page.route("**/v1/notes/format-turns", (route) =>
    route.fulfill(json({ limit: 5, used: 5 - left(), left: left() })),
  );
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
    test.describe(`voice note format ${vp.name} ${scheme}`, () => {
      test.use({
        viewport: { width: vp.width, height: vp.height },
        hasTouch: true,
        isMobile: true,
        colorScheme: scheme,
      });

      test("the rail icon shows the free formats left as a badge, 5 then 1, hidden at 0", async ({
        page,
        request,
      }, info) => {
        let left = 5;
        const { state } = await setup(page, request, vp, "vnf-badge");
        await mock(page, { transcript: RAW });
        await freeLeft(page, () => left);
        await openLesson(page, state.lesson);
        await page.waitForTimeout(600);
        await enterImmersive(page);
        await openRail(page);
        const badge = page.getByTestId("voice-note-free-left");
        await expect(badge).toHaveText("5");
        await expect(badge).toHaveAttribute(
          "aria-label",
          "5 free formats left",
        );
        await snap(page, info, vp, scheme, "5-rail-badge-5");
        left = 1;
        await page.reload();
        await openLesson(page, state.lesson);
        await page.waitForTimeout(600);
        await enterImmersive(page);
        await openRail(page);
        await expect(badge).toHaveText("1");
        await expect(badge).toHaveAttribute("aria-label", "1 free format left");
        await snap(page, info, vp, scheme, "6-rail-badge-1");
        left = 0;
        await page.reload();
        await openLesson(page, state.lesson);
        await page.waitForTimeout(600);
        await enterImmersive(page);
        await openRail(page);
        await expect(page.getByTestId("immersive-voice-note")).toBeVisible();
        await expect(badge).toHaveCount(0);
      });

      test("past the free formats the Saved toast offers Format and asks only when pressed", async ({
        page,
        request,
      }, info) => {
        const { state } = await setup(page, request, vp, "vnf-toast");
        await mock(page, { transcript: RAW });
        await freeLeft(page, () => 0);
        const formatted: string[] = [];
        await page.route("**/v1/notes", (route) =>
          route.request().method() === "POST"
            ? route.fulfill(
                json({
                  id: "voice-note-1",
                  status: "raw_only",
                  free_formats_left: 0,
                  format_offer: true,
                }),
              )
            : route.fallback(),
        );
        await page.route("**/v1/notes/voice-note-1/format", (route) => {
          formatted.push(route.request().url());
          return route.fulfill(
            json({ id: "voice-note-1", status: "enrichment_pending" }),
          );
        });
        await openLesson(page, state.lesson);
        await page.waitForTimeout(600);
        await enterImmersive(page);
        await openRail(page);
        await press(page);
        await page.waitForTimeout(800);
        await page.mouse.up();
        await expect(page.getByTestId("toast")).toContainText(/Saved/i);
        await expect(page.getByTestId("toast-action")).toHaveText("Format");
        expect(formatted).toHaveLength(0);
        await expectToastClearOfTitle(page);
        // One line on a 390 px screen: the message and its action share a row.
        const row = await page.getByTestId("toast").boundingBox();
        expect(row!.height).toBeLessThan(64);
        await page.waitForTimeout(500); // past the toast's fade-in
        await snap(page, info, vp, scheme, "7-toast-format");
        await page.getByTestId("toast-action").click();
        await expect.poll(() => formatted.length).toBe(1);
      });

      test("the Notes page shows the formatted note with Show original", async ({
        page,
        request,
      }, info) => {
        await setup(page, request, vp, "vnf-notes");
        const now = new Date().toISOString();
        await page.route("**/v1/notes", (route) =>
          route.request().method() === "GET"
            ? route.fulfill(
                json({
                  notes: [
                    {
                      id: "voice-note-2",
                      action_key: "k-2",
                      source: "slide",
                      slide_id: 0,
                      slide_title: "Unbuffered channels",
                      raw_text: RAW,
                      status: "enriched",
                      title: "Unbuffered channels",
                      formatted_body:
                        "- An unbuffered channel blocks the sender until a receiver is ready.",
                      anchor: {
                        playback_ms: 1200,
                        input: "voice",
                        source_state: "unavailable",
                      },
                      version: 1,
                      created_at: now,
                      updated_at: now,
                    },
                  ],
                }),
              )
            : route.fallback(),
        );
        await page.goto(`${webBase}/notes`);
        await page.getByText("Unbuffered channels").first().click();
        const formatted = page.getByTestId("note-formatted");
        await expect(formatted).toContainText("unbuffered channel blocks");
        const original = page.locator("p", { hasText: "chanel" });
        await expect(original).toBeHidden();
        await snap(page, info, vp, scheme, "8-notes-formatted");
        await page.getByTestId("note-show-original").click();
        await expect(original).toBeVisible();
        await expect(page.getByTestId("note-show-original")).toHaveText(
          "Hide original",
        );
        await snap(page, info, vp, scheme, "9-notes-original");
      });
    });
  }
}
