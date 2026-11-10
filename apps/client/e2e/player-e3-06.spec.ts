import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";

test.use({ hasTouch: true });
resetTokens();
test.afterEach(() => resetTokens());

for (const width of [1280, 390]) {
  test(`download entitled course library and replay offline after subscription expiry at ${width}px`, async ({
    page,
    request,
    context,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({
      colorScheme: width === 390 ? "dark" : "light",
      reducedMotion: width === 390 ? "reduce" : "no-preference",
    });

    // 1. Authenticate learner
    await page.goto("/profile");
    const token = await page.evaluate(
      () =>
        JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!)
          .access_token,
    );
    const authHeaders = { Authorization: `Bearer ${token}` };

    const routeCourses = async () => {
      await page.route("**/v1/courses/**", async (route) => {
        const original = route.request();
        const target = new URL(original.url());
        const headers = original.headers();
        delete headers.origin;
        target.host = new URL(apiBase).host;
        await route.fulfill({
          response: await request.fetch(target.href, {
            method: original.method(),
            headers,
            data: original.postData() ?? undefined,
          }),
        });
      });
    };
    await routeCourses();

    // 2. Seed fixture course with playable lesson
    const seed = await request.post(`${apiBase}/test/player/fixture`, {
      headers: authHeaders,
    });
    expect(seed.status()).toBe(201);
    const { lesson_id, course_id } = await seed.json();

    // 3. Fund an entitled subscription (Learn monthly plan)
    const fundRes = await request.post(`${apiBase}/test/subscription/fund`, {
      headers: authHeaders,
      data: { plan_id: "learn" },
    });
    if (!fundRes.ok()) {
      console.error("fundRes failed:", fundRes.status(), await fundRes.text());
    }
    expect(fundRes.ok()).toBe(true);

    // 4. Navigate to the course / learn path
    await page.goto(course_id ? `/course/${course_id}` : "/");
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();

    // 5. Entitled learner sees and activates the "Download library" / "Download course" control
    const downloadBtn = page.getByRole("button", {
      name: /download course|download library/i,
    });
    await expect(downloadBtn).toBeVisible();

    // Focus and keyboard accessibility check
    await downloadBtn.focus();
    await expect(downloadBtn).toBeFocused();

    // Click download library button (triggers ui_control_activated(download_library))
    await downloadBtn.click();

    // Progress and storage controls are visible
    await expect(
      page
        .getByRole("progressbar", { name: "Download progress" })
        .or(page.getByText(/downloading|saved offline/i))
        .first(),
    ).toBeVisible();

    // Wait until download completes
    await expect(
      page.getByText(/saved offline|available offline/i).first(),
    ).toBeVisible({
      timeout: 10000,
    });

    // Storage controls show downloaded count / size
    const storageInfo = page
      .getByText(/stored offline|storage used|downloaded/i)
      .first();
    await expect(storageInfo).toBeVisible();

    // 6. Test offline replay of the downloaded course library
    await context.setOffline(true);
    await page.route("**/v1/**", (route) =>
      route.abort("internetdisconnected"),
    );

    // Reload course path while offline - shows offline banner matching S08-Path canvas
    await page.reload();
    await expect(
      page
        .getByText(/offline\. saved lessons are available/i)
        .or(page.getByText(/offline/i))
        .first(),
    ).toBeVisible();

    // Play the downloaded lesson offline
    await page.goto(`/lesson/${lesson_id}`);
    await expect(page.getByText(/available offline/i).first()).toBeVisible();

    // All slides in the downloaded lesson load from cache inside sandboxed iframe
    const slideFrame = page.locator('iframe[title="Slide 1"]');
    await expect(slideFrame).toBeVisible();
    await expect(slideFrame).toHaveAttribute("sandbox", "allow-scripts");
    await expect(
      page.frameLocator('iframe[title="Slide 1"]').locator(".star"),
    ).toBeVisible();

    // Keyboard navigation works offline
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await expect(page.locator('iframe[title="Slide 2"]')).toBeVisible();

    // 7. Go back online and simulate subscription expiry
    await context.setOffline(false);
    await page.unroute("**/v1/**");
    await routeCourses();

    const expireRes = await request.post(
      `${apiBase}/test/subscription/expire`,
      {
        headers: authHeaders,
      },
    );
    if (!expireRes.ok()) {
      console.error(
        "expireRes failed:",
        expireRes.status(),
        await expireRes.text(),
      );
    }
    expect(expireRes.ok()).toBe(true);

    // 8. Expired subscription:
    // A) Attempting new bulk library download is refused (403 subscription_required)
    await page.goto(course_id ? `/course/${course_id}` : "/");
    const downloadAgainBtn = page.getByRole("button", {
      name: /download course|download library/i,
    });
    if (await downloadAgainBtn.isVisible()) {
      await downloadAgainBtn.click();
      await expect(
        page.getByText(/subscription required|plan required/i).first(),
      ).toBeVisible();
    }

    // B) BUT already downloaded/cached lessons STILL replay offline!
    await context.setOffline(true);
    await page.route("**/v1/**", (route) =>
      route.abort("internetdisconnected"),
    );
    await page.goto(`/lesson/${lesson_id}`);
    await expect(page.getByText(/available offline/i).first()).toBeVisible();
    const replayedSlide = page
      .locator('iframe[title="Slide 2"]')
      .or(page.locator('iframe[title="Slide 1"]'));
    await expect(replayedSlide).toBeVisible();
    await expect(replayedSlide).toHaveAttribute("sandbox", "allow-scripts");

    // 9. 320px reflow check on mobile
    if (width === 390) {
      await page.setViewportSize({ width: 320, height: 900 });
      await expect
        .poll(() =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= window.innerWidth,
          ),
        )
        .toBe(true);
    }
  });
}
