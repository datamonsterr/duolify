import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";

resetTokens();
test.afterEach(() => resetTokens());

const scripts = (urls: string[]) =>
  urls.filter((u) => /\/assets\/[^/]+\.js$/.test(new URL(u).pathname));
const heavy =
  /\/(LessonPlayer|LessonTest|MarketplacePage|MarketCoursePage|ModeratorQueue|module\.no-external|clerk)-[^/]+\.js$/;

async function seedLesson(
  page: import("@playwright/test").Page,
  request: import("@playwright/test").APIRequestContext,
) {
  await page.goto("/profile");
  const token = await page.evaluate(
    () =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!)
        .access_token,
  );
  const seed = await request.post(`${apiBase}/test/player/fixture`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(seed.status()).toBe(201);
  return (await seed.json()).lesson_id as string;
}

test("first load skips the player, marketplace and analytics chunks; a lesson loads its own", async ({
  page,
  request,
}) => {
  const lessonId = await seedLesson(page, request);
  const requested: string[] = [];
  page.on("request", (r) => requested.push(r.url()));
  await page.goto("/");
  await expect(page.getByRole("navigation").first()).toBeVisible();
  await page.waitForLoadState("networkidle");
  const first = scripts(requested);
  expect(first.length).toBeGreaterThan(0);
  expect(first.filter((u) => heavy.test(new URL(u).pathname))).toEqual([]);

  requested.length = 0;
  await page.goto(`/lesson/${lessonId}`);
  await expect(page.getByText("Slide 1 of 3")).toBeVisible();
  expect(
    scripts(requested).some((u) => /\/LessonPlayer-[^/]+\.js$/.test(u)),
  ).toBe(true);
});

test("an opened lesson's route chunks are cached for offline use", async ({
  page,
  request,
  context,
}) => {
  const lessonId = await seedLesson(page, request);
  await page.goto(`/lesson/${lessonId}`);
  await expect(page.getByText("Available offline")).toBeVisible();
  await page.evaluate(async () => {
    await navigator.serviceWorker.ready;
  });
  // The player asks the worker to cache the lesson-test and course-list chunks too.
  await page.waitForFunction(async () => {
    const names = await caches.keys();
    const urls: string[] = [];
    for (const name of names) {
      const cache = await caches.open(name);
      urls.push(...(await cache.keys()).map((r) => new URL(r.url).pathname));
    }
    return (
      urls.some((u) => /\/LessonPlayer-[^/]+\.js$/.test(u)) &&
      urls.some((u) => /\/LessonTest-[^/]+\.js$/.test(u)) &&
      urls.some((u) => /\/Courses-[^/]+\.js$/.test(u))
    );
  });
  // Not everything is precached: an unrelated route chunk is not in the cache.
  const marketplaceCached = await page.evaluate(async () => {
    for (const name of await caches.keys()) {
      const keys = await (await caches.open(name)).keys();
      if (keys.some((r) => /\/MarketplacePage-/.test(r.url))) return true;
    }
    return false;
  });
  expect(marketplaceCached).toBe(false);

  await context.setOffline(true);
  await page.route("**/v1/**", (route) => route.abort("internetdisconnected"));
  await page.reload();
  await expect(page.getByText("Available offline")).toBeVisible();
  await expect(page.locator('iframe[title="Slide 1"]')).toBeVisible();
});
