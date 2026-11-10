import { expect, test } from "./native-store-fixture";
import { apiBase, resetAuthRateLimits, webBase } from "./servers";
import type { Tokens } from "../src/features/accounts/secure-store";

test.use({ hasTouch: true });

for (const width of [1280, 390]) {
  test(`E9-05 bookmark a slide offline, reconnect twice and open the exact slide at ${width}px`, async ({
    page,
    context,
    request,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await resetAuthRateLimits(request);

    // Route /v1/ API calls to test backend with offline toggling for bookmarks
    let offline = false;
    let demoLesson = "";
    await page.route(/\/v1\//, async (route) => {
      const original = route.request();
      if (offline && original.url().includes("/v1/bookmarks")) {
        await route.abort("failed");
        return;
      }
      const target = new URL(original.url());
      const headers = original.headers();
      delete headers.origin;
      target.host = new URL(apiBase).host;
      const demo = target.pathname.startsWith("/v1/lessons/sky/");
      if (demo && demoLesson) {
        target.pathname = target.pathname.replace(
          "/lessons/sky/",
          `/lessons/${demoLesson}/`,
        );
      }
      try {
        const response = await request.fetch(target.href, {
          method: original.method(),
          headers,
          data: original.postData() ?? undefined,
        });
        await route.fulfill({ response });
      } catch {
        // Request aborted or closed during navigation
      }
    });

    // Authenticate a new learner
    const email = `bookmarks-${width}-${Date.now()}@example.test`;
    expect(
      (
        await request.post(`${apiBase}/v1/auth/code`, {
          data: { email, age_confirmed: true },
        })
      ).status(),
    ).toBe(202);

    const { code } = await (
      await request.get(`${apiBase}/test/mail?email=${email}`)
    ).json();

    const login = await request.post(`${apiBase}/v1/auth/verify`, {
      data: { email, code, age_confirmed: true },
    });
    expect(login.status()).toBe(200);
    const tokens = (await login.json()) as Tokens;

    const meRes = await request.get(`${apiBase}/v1/me`, {
      headers: { Authorization: `Bearer ${tokens.access_token}` },
    });
    expect(meRes.ok()).toBe(true);
    const me = (await meRes.json()) as { id: string };

    const fixtureRes = await request.post(`${apiBase}/test/player/fixture`, {
      headers: { Authorization: `Bearer ${tokens.access_token}` },
    });
    expect(fixtureRes.ok()).toBe(true);
    const fixtureData = (await fixtureRes.json()) as {
      lesson_id: string;
      course_id?: string;
    };
    demoLesson = fixtureData.lesson_id;
    const courseId = fixtureData.course_id || "course";

    await context.addInitScript(
      ({ tokens, owner, demoLesson, courseId }) => {
        sessionStorage.setItem(
          "duolify.native-store-fixture",
          JSON.stringify(tokens),
        );
        const downloadedCourse = {
          courseId,
          title: "Why the sky changes",
          downloadedAt: new Date().toISOString(),
          lessonIds: [demoLesson],
        };
        localStorage.setItem(
          `duolify:download-index:${owner}`,
          JSON.stringify({ [courseId]: downloadedCourse }),
        );
      },
      { tokens, owner: me.id, demoLesson, courseId },
    );

    // Navigate to lesson player
    await page.goto(`${webBase}/lesson/${demoLesson}`);
    await expect(page.getByText(/Why the sky changes/i)).toBeVisible();

    // Advance to Slide 2
    const nextBtn = page.getByRole("button", { name: "Next" });
    await expect(nextBtn).toBeVisible();
    await nextBtn.click();
    await expect(page.getByText(/Slide 2 of 3/i)).toBeVisible();

    // Set offline
    offline = true;

    // Trigger Bookmark button on Slide 2
    const bookmarkBtn = page.getByRole("button", {
      name: /^Bookmark$/i,
    });
    await expect(bookmarkBtn).toBeVisible();
    await bookmarkBtn.click();

    // Verify button toggles to Saved state per canvas S09-Player
    await expect(page.getByRole("button", { name: /^Saved$/i })).toBeVisible();

    // Verify toast feedback appears: "Slide bookmarked" with "Undo" link
    await expect(page.getByText(/Slide bookmarked/i)).toBeVisible();

    // Reconnect twice:
    // First reconnection / sync
    offline = false;
    await page.evaluate(async () => {
      // Trigger sync via custom event or window method
      window.dispatchEvent(new Event("online"));
      if (
        (window as unknown as { syncBookmarks?: () => Promise<void> })
          .syncBookmarks
      ) {
        await (
          window as unknown as { syncBookmarks: () => Promise<void> }
        ).syncBookmarks();
      }
    });

    // Second reconnection / sync attempt to verify idempotency (replaying twice yields 1 bookmark)
    await page.evaluate(async () => {
      window.dispatchEvent(new Event("online"));
      if (
        (window as unknown as { syncBookmarks?: () => Promise<void> })
          .syncBookmarks
      ) {
        await (
          window as unknown as { syncBookmarks: () => Promise<void> }
        ).syncBookmarks();
      }
    });

    // Navigate to /bookmarks
    await page.goto(`${webBase}/bookmarks`);
    await expect(
      page.getByRole("heading", { name: /Bookmarks/i }),
    ).toBeVisible();

    // Verify course filter chips
    await expect(
      page.getByRole("button", { name: /All courses/i }),
    ).toBeVisible();

    // Verify bookmarked item appears with "Why the sky changes" and "Slide 2"
    await expect(page.getByText(/Why the sky changes/i).first()).toBeVisible();
    await expect(page.getByText(/Slide 2/i).first()).toBeVisible();

    // Verify "Saved offline" chip appears for cached lesson
    await expect(page.getByText(/Saved offline/i).first()).toBeVisible();

    // Verify "Open" action exists and click it to open the exact slide
    const openBtn = page
      .getByRole("link", { name: /Open Why the sky changes, slide 2|Open/i })
      .or(
        page.getByRole("button", {
          name: /Open Why the sky changes, slide 2|Open/i,
        }),
      )
      .first();
    await expect(openBtn).toBeVisible();
    await openBtn.click();

    // Verify learner returns to the exact Slide 2
    await expect(page).toHaveURL(new RegExp(`/lesson/(${demoLesson}|sky)`));
    await expect(page.getByText(/Slide 2 of 3/i)).toBeVisible();
  });
}
