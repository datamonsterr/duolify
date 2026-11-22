// Stage contract: a framed Slide is scaled uniformly to its 16:9 stage, never clipped.
import { copyFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";

resetTokens();
test.afterEach(() => resetTokens());
const SHOTS =
  "/home/dat/.cache/tmp/claude-1000/-home-dat-dev-duolify/3c11ae02-db6a-44d9-9754-88e15e8d834c/scratchpad/immersive-shots";

for (const width of [390, 844, 1280]) {
  test(`framed Slide keeps 16:9 and fits its frame at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
    await page.goto("/lesson/sky");
    const frame = page.locator('iframe[title="Slide 1"]');
    await expect(frame).toBeVisible();
    await page.waitForTimeout(400);
    const stage = (await page.locator(".stage-frame").boundingBox())!;
    const box = (await frame.boundingBox())!;
    expect(box.width / box.height).toBeCloseTo(16 / 9, 1);
    // the whole Slide is inside the stage: letterboxed, not clipped
    expect(box.x).toBeGreaterThanOrEqual(stage.x - 1);
    expect(box.y).toBeGreaterThanOrEqual(stage.y - 1);
    expect(box.x + box.width).toBeLessThanOrEqual(stage.x + stage.width + 1);
    expect(box.y + box.height).toBeLessThanOrEqual(stage.y + stage.height + 1);
    expect(stage.width).toBeLessThanOrEqual(width);
    const file = info.outputPath(`framed-${width}.png`);
    await page.screenshot({ path: file });
    try {
      mkdirSync(SHOTS, { recursive: true });
      copyFileSync(file, join(SHOTS, `framed-after-${width}.png`));
    } catch {
      /* owner copy is best effort */
    }
  });
}

test("authored example lesson is not clipped at 390px (JavaScript Essentials, lesson 1)", async ({
  page,
  request,
}, info) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/profile");
  const token = await page.evaluate(
    () =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!)
        .access_token,
  );
  const headers = { Authorization: `Bearer ${token}` };
  const enroll = await request.post(
    `${apiBase}/v1/courses/examples/javascript-essentials/enroll`,
    { headers },
  );
  expect(enroll.ok()).toBe(true);
  const { course_id } = await enroll.json();
  const course = await (
    await request.get(`${apiBase}/v1/courses/${course_id}`, { headers })
  ).json();
  await page.goto(`/lesson/${course.units[0].lessons[0].id}`);
  const frame = page.locator('iframe[title="Slide 1"]');
  await expect(frame).toBeVisible();
  await page.waitForTimeout(600);
  const stage = (await page.locator(".stage-frame").boundingBox())!;
  const box = (await frame.boundingBox())!;
  expect(box.width / box.height).toBeCloseTo(16 / 9, 1);
  expect(box.x + box.width).toBeLessThanOrEqual(stage.x + stage.width + 1);
  expect(box.y + box.height).toBeLessThanOrEqual(stage.y + stage.height + 1);
  expect(stage.width).toBeLessThanOrEqual(390);
  // The Slide document is laid out in the 1280×720 stage: every card fits inside it.
  const content = await page
    .frameLocator('iframe[title="Slide 1"]')
    .locator("body")
    .evaluate(() => ({
      scrollHeight: document.documentElement.scrollHeight,
      scrollWidth: document.documentElement.scrollWidth,
    }));
  expect(content.scrollHeight).toBeLessThanOrEqual(721);
  expect(content.scrollWidth).toBeLessThanOrEqual(1281);
  const file = info.outputPath("javascript-lesson-1-390.png");
  await page.screenshot({ path: file });
  try {
    mkdirSync(SHOTS, { recursive: true });
    copyFileSync(file, join(SHOTS, "javascript-lesson-1-390-after.png"));
  } catch {
    /* owner copy is best effort */
  }
});
