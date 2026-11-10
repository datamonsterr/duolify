import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";

resetTokens();
test.afterEach(() => resetTokens());

for (const width of [1280, 390]) {
  test(`offline playback queues and reconciles once on reconnect at ${width}px`, async ({
    page,
    request,
    context,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({
      colorScheme: width === 390 ? "dark" : "light",
      reducedMotion: width === 390 ? "reduce" : "no-preference",
    });

    await page.goto("/profile");
    const token = await page.evaluate(
      () =>
        JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!)
          .access_token,
    );
    const headers = { Authorization: `Bearer ${token}` };
    const seed = await request.post(`${apiBase}/test/player/fixture`, {
      headers,
    });
    expect(seed.status()).toBe(201);
    const { lesson_id } = await seed.json();

    // 1. Open lesson online and let it cache
    await page.goto(`/lesson/${lesson_id}`);
    await expect(page.getByText("Slide 1 of 3")).toBeVisible();
    await expect(page.getByText("Available offline")).toBeVisible();

    // 2. Go offline
    await context.setOffline(true);
    let syncRequests = 0;
    await page.route("**/v1/lessons/*/sync", async (route) => {
      syncRequests++;
      await route.abort("internetdisconnected");
    });
    await page.route("**/v1/lessons/*/position", async (route) => {
      await route.abort("internetdisconnected");
    });

    // 3. Progress while offline: advance to slide 2
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await expect(page.getByText("Slide 2 of 3")).toBeVisible();
    await expect(page.locator('iframe[title="Slide 2"]')).toBeVisible();

    // Verify "Waiting to sync" is visibly displayed
    await expect(page.getByText("Waiting to sync")).toBeVisible();

    // Restart offline: reload page while still offline
    await page.reload();
    await expect(page.getByText("Slide 2 of 3")).toBeVisible();
    await expect(page.getByText("Waiting to sync")).toBeVisible();

    // 4. Reconnect twice and observe one progress action
    await page.unroute("**/v1/lessons/*/sync");
    await page.unroute("**/v1/lessons/*/position");
    syncRequests = 0;
    const handledKeys = new Set<string>();

    await page.route("**/v1/lessons/*/sync", async (route) => {
      syncRequests++;
      const original = route.request();
      const postData = original.postDataJSON();
      const actionKey =
        postData?.action_key || postData?.events?.[0]?.action_key;
      if (actionKey) handledKeys.add(actionKey);

      const target = new URL(original.url());
      const headers = original.headers();
      delete headers.origin;
      target.host = new URL(apiBase).host;
      const response = await request.fetch(target.href, {
        method: original.method(),
        headers,
        data: original.postData() ?? undefined,
      });
      await route.fulfill({ response });
    });

    // First reconnect
    await context.setOffline(false);
    await page.evaluate(() => window.dispatchEvent(new Event("online")));

    // "Waiting to sync" should clear after canonical reconciliation
    await expect(page.getByText("Waiting to sync")).toHaveCount(0, {
      timeout: 10000,
    });
    expect(syncRequests).toBeGreaterThanOrEqual(1);
    expect(handledKeys.size).toBe(1);

    // Second reconnect (double reconnect/retry)
    const initialSyncs = syncRequests;
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await page.waitForTimeout(500);
    expect(syncRequests).toBe(initialSyncs);

    // Server canonical position check
    const positionRes = await request.get(
      `${apiBase}/v1/lessons/${lesson_id}/position`,
      { headers },
    );
    expect(positionRes.status()).toBe(200);
    const positionBody = await positionRes.json();
    expect(positionBody.slide_id).toBe("sky-2");

    // 5. Stable slide deep link: resolves without implying completion
    await page.goto(`/lesson/${lesson_id}?slide=sky-3`);
    await expect(page.getByText("Slide 3 of 3")).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Take lesson test" }),
    ).toBeVisible();
    await expect(page.getByText("Lesson complete!")).toHaveCount(0);

    // Invalid deep link falls back safely
    await page.goto(`/lesson/${lesson_id}?slide=nonexistent-slide`);
    await expect(page.getByText("Slide 1 of 3")).toBeVisible();

    await page.screenshot({
      path: testInfo.outputPath(`offline-sync-${width}.png`),
      fullPage: true,
    });
  });
}
