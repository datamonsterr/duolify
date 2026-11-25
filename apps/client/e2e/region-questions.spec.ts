// Region questions UI (immersive-mobile-lesson-player 4.1, 4.2, 4.7, 4.9): select an area, ask by
// text, the answer panel, save as an anchored Quick note, collapse, close. Mocked suite: the
// tutor endpoints are fulfilled here, everything else goes to the fixture API.
import { join } from "node:path";
import type { Page, Route } from "@playwright/test";
import { expect, test } from "./native-store-fixture";
import {
  OWNER_SHOTS,
  VIEWPORTS,
  clock,
  enterImmersive,
  openLesson,
  openRail,
  setup,
  shot,
  type Vp,
} from "./anchored-helpers";

const SHOTS = join(OWNER_SHOTS, "..", "region-questions-shots");
const snap = (
  page: Page,
  info: Parameters<typeof shot>[1],
  vp: Vp,
  name: string,
) => shot(page, info, vp, name, SHOTS);

const REPLY = "Because the light is scattered by the air.";
type Seen = {
  quote: Record<string, unknown>[];
  messages: Record<string, unknown>[];
};

async function mockTutor(
  page: Page,
  lesson: string,
  opts: { anchored?: boolean } = {},
): Promise<Seen> {
  const seen: Seen = { quote: [], messages: [] };
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
    route.fulfill({
      json: opts.anchored
        ? {
            thread: { id: "11111111-1111-4111-8111-111111111111" },
            messages: [
              {
                id: "old-q",
                role: "user",
                content: "What is this part?",
                status: "complete",
                created_at: "2030-01-01T00:00:00Z",
                anchor: {
                  lesson_id: lesson,
                  slide_index: 0,
                  slide_id: "s0",
                  at_ms: 1000,
                  rect: { x: 0.1, y: 0.2, w: 0.3, h: 0.3 },
                  region_ids: ["r1"],
                  grounding: "regions",
                },
              },
            ],
          }
        : { thread: null, messages: [] },
    }),
  );
  await page.route("**/v1/courses/*/tutor/personas*", (route) =>
    route.fulfill({ status: 404, json: { error: "not_found" } }),
  );
  await page.route("**/v1/courses/*/tutor/quote", async (route: Route) => {
    seen.quote.push(JSON.parse(route.request().postData() ?? "{}"));
    await route.fulfill({
      json: {
        id: "q1",
        content_ceiling: 2,
        pricing_version: "v1",
        daily_free_eligible: true,
        expires_at: new Date(Date.now() + 600_000).toISOString(),
      },
    });
  });
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

for (const vp of VIEWPORTS.filter((v) => v.name !== "portrait-390x844")) {
  test.describe(`region question at ${vp.name}`, () => {
    test.use({
      viewport: { width: vp.width, height: vp.height },
      hasTouch: vp.width < 1000,
      isMobile: vp.width < 1000,
    });

    test("select an area, ask by text, answer streams in the panel, save as note, swipe to close, close resumes", async ({
      page,
      request,
    }, info) => {
      const { posted, state } = await setup(page, request, vp, "ask");
      const seen = await mockTutor(page, state.lesson);
      await page.context().addInitScript(() => {
        (window as unknown as { __analyticsLog: unknown[] }).__analyticsLog =
          [];
      });
      await openLesson(page, state.lesson);
      await page.waitForTimeout(700);
      await enterImmersive(page);
      await openRail(page);
      await page.getByTestId("immersive-select-area").click();
      await expect(page.getByTestId("region-box")).toBeFocused();
      // Move the area to the right where the panel will cover the stage.
      for (let i = 0; i < 4; i++) await page.keyboard.press("Alt+ArrowRight");
      await snap(page, info, vp, "1-area-selected");
      const t0 = await clock(page);
      await page.getByTestId("region-ask").click();

      const panel = page.getByTestId("answer-panel");
      await expect(panel).toBeVisible();
      await expect(page.getByTestId("region-outline")).toBeVisible();
      await expect(
        panel.getByRole("textbox", { name: "Your question" }),
      ).toBeFocused();
      // The panel is opaque and the region stays visible: the stage shifted left.
      await page.waitForTimeout(500); // past the stage shift transition
      const outline = (await page.getByTestId("region-outline").boundingBox())!;
      const box = (await panel.boundingBox())!;
      expect(outline.x + outline.width).toBeLessThanOrEqual(box.x + 1);
      // Captions never sit under the panel.
      const caption = page.getByTestId("immersive-caption");
      if (await caption.isVisible()) {
        const c = (await caption.boundingBox())!;
        expect(c.x + c.width).toBeLessThanOrEqual(box.x + 1);
      }
      await page.waitForTimeout(400);
      expect(await clock(page)).toBe(t0);
      await snap(page, info, vp, "2-panel-open");

      await panel
        .getByRole("textbox", { name: "Your question" })
        .fill("Why is this orange?");
      await panel.getByTestId("answer-send").click();
      await expect(panel.getByText(REPLY)).toBeVisible();
      await expect(panel.getByText("Why is this orange?")).toBeVisible();
      // Design review: the composer never squeezes the reply out of a short (landscape phone) panel.
      await expect(panel.getByText(REPLY)).toBeInViewport({ ratio: 1 });
      expect(
        (await panel.locator(".imm-answer-log").boundingBox())!.height,
      ).toBeGreaterThanOrEqual(80);
      await snap(page, info, vp, "3-reply");

      // Same region on the quote and the message, next to the lesson.
      expect(seen.quote).toHaveLength(1);
      expect(seen.messages).toHaveLength(1);
      for (const body of [seen.quote[0], seen.messages[0]]) {
        expect(body.lesson).toEqual({
          lesson_id: state.lesson,
          slide_index: 0,
        });
        const region = body.region as {
          rect: { x: number; w: number };
          at_ms: number;
          input: string;
        };
        expect(region.input).toBe("text");
        expect(region.rect.w).toBeGreaterThan(0.06);
        expect(region.at_ms).toBeGreaterThanOrEqual(0);
      }
      expect(seen.messages[0].region).toEqual(seen.quote[0].region);

      // Save as an anchored Quick note.
      await panel.getByTestId("answer-save-note").click();
      await expect(page.getByTestId("toast")).toContainText(
        "Saved as Quick note",
      );
      const note = posted.find((p) =>
        String(p.body.action_key).startsWith("region-answer:"),
      )!;
      expect(note.body.raw_text).toBe(REPLY);
      expect((note.body.anchor as { rect: object }).rect).toBeTruthy();

      // Analytics: the area selection is captured on Ask, with enums and ids only.
      const log = () =>
        page.evaluate(
          () =>
            (
              window as unknown as {
                __analyticsLog?: {
                  event: string;
                  properties: Record<string, unknown>;
                }[];
              }
            ).__analyticsLog ?? [],
        );
      const selected = (await log()).filter(
        (e) => e.event === "lesson_region_selected",
      );
      expect(selected).toHaveLength(1);
      expect(selected[0].properties).toMatchObject({
        source: "client",
        lesson_id: state.lesson,
        slide_index: 0,
        method: "keyboard", // Select area, then moved with the arrow keys
      });
      expect(["small", "medium", "large"]).toContain(
        selected[0].properties.area_bucket,
      );

      // Swiping the panel toward its edge closes it; the sidebar has no close or collapse buttons.
      await expect(panel.getByTestId("answer-collapse")).toHaveCount(0);
      const grab = await panel.locator(".imm-answer-grab").boundingBox();
      if (!grab) throw new Error("grab handle missing");
      await page.mouse.move(grab.x + 2, grab.y + 20);
      await page.mouse.down();
      await page.mouse.move(grab.x + 120, grab.y + 22, { steps: 6 });
      await snap(page, info, vp, "3b-mid-swipe");
      await page.mouse.move(grab.x + 320, grab.y + 22, { steps: 6 });
      await page.mouse.up();
      await expect(page.getByTestId("answer-panel")).toHaveCount(0);
      await page.getByTestId("immersive-ask").click();
      await expect(page.getByTestId("answer-panel")).toBeVisible();

      // Close resumes playback.
      await page.getByTestId("answer-close").dispatchEvent("click");
      await expect(page.getByTestId("answer-panel")).toHaveCount(0);
      const closed = (await log()).filter(
        (e) => e.event === "lesson_answer_panel_closed",
      );
      expect(closed).toHaveLength(2);
      expect(closed[0].properties).toMatchObject({
        outcome: "completed",
        saved_note: true,
        follow_ups: "0",
        lesson_id: state.lesson,
      });
      expect(JSON.stringify(closed[0].properties)).not.toContain("orange");
      // The fixture Slide is short, so resuming may also restart it: the clock must move either way.
      await expect.poll(() => clock(page)).not.toBe(t0);
      await snap(page, info, vp, "4-closed-resumed");
    });

    test("a question about the whole Slide sends no region", async ({
      page,
      request,
    }) => {
      const { state } = await setup(page, request, vp, "whole");
      const seen = await mockTutor(page, state.lesson);
      await openLesson(page, state.lesson);
      await page.waitForTimeout(500);
      await enterImmersive(page);
      await openRail(page);
      await page.getByTestId("immersive-ask").click();
      await page
        .getByRole("textbox", { name: "Your question" })
        .fill("What is this slide about?");
      await page.getByTestId("answer-send").click();
      await expect(page.getByText(REPLY)).toBeVisible();
      expect(seen.messages[0].lesson).toBeTruthy();
      expect("region" in seen.messages[0]).toBe(false);
    });

    test("an earlier anchored question restores its outline", async ({
      page,
      request,
    }, info) => {
      const { state } = await setup(page, request, vp, "restore");
      await mockTutor(page, state.lesson, { anchored: true });
      await openLesson(page, state.lesson);
      await page.waitForTimeout(500);
      await enterImmersive(page);
      await openRail(page);
      await page.getByTestId("immersive-ask").click();
      await page.getByTestId("answer-restore").click();
      await expect(page.getByTestId("region-outline")).toBeVisible();
      await snap(page, info, vp, "5-restored-outline");
    });
  });
}

test.describe("narrow, unrotated screens use the lesson tutor sheet", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: false });

  test("the question opens the tutor sheet with the area as a thumbnail", async ({
    page,
    request,
  }, info) => {
    const vp = VIEWPORTS[0];
    const { state } = await setup(page, request, vp, "sheet");
    const seen = await mockTutor(page, state.lesson);
    await openLesson(page, state.lesson);
    await page.waitForTimeout(500);
    await enterImmersive(page);
    await openRail(page);
    await page.getByTestId("immersive-select-area").click();
    await page.getByTestId("region-ask").click();
    await expect(page.getByTestId("answer-panel")).toHaveCount(0);
    const sheet = page
      .getByRole("dialog", { name: "Ask tutor" })
      .or(page.getByRole("region", { name: "Ask tutor" }));
    await expect(sheet.first()).toBeVisible();
    await expect(page.getByTestId("region-thumb")).toBeVisible();
    await snap(page, info, vp, "6-portrait-sheet");
    await page.getByRole("textbox").first().fill("What is this?");
    await page.getByRole("button", { name: /send/i }).first().click();
    await expect(page.getByText(REPLY)).toBeVisible();
    expect((seen.messages[0].region as { rect: object }).rect).toBeTruthy();
  });
});
