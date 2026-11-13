import { test, expect } from "./accounts-fixture";

const sampleAnalytics = {
  plan_id: "plus_monthly",
  plan_name: "Plus",
  entitlement_version: "usd-quotes-2026-11-09",
  time_zone: "Asia/Ho_Chi_Minh",
  local_date: "2026-11-13",
  as_of: "2026-11-13T13:30:00Z",
  custom_export_allowed: false,
  summary: {
    total_xp: 240,
    total_lessons_completed: 12,
    perfect_completions: 6,
    total_duration_ms: 720000,
    current_streak: 7,
    longest_streak: 23,
    freezes_held: 1,
    freezes_used: 1,
  },
  course_breakdown: [
    {
      course_id: "c1111111-1111-4111-8111-111111111111",
      title: "Astronomy Basics",
      completed_lessons: 8,
      total_xp: 160,
      perfect_completions: 4,
      total_duration_ms: 480000,
      last_completed_at: "2026-11-12T10:00:00Z",
    },
    {
      course_id: "c2222222-2222-4222-8222-222222222222",
      title: "Plant Care",
      completed_lessons: 4,
      total_xp: 80,
      perfect_completions: 2,
      total_duration_ms: 240000,
      last_completed_at: "2026-11-13T08:00:00Z",
    },
  ],
  daily_xp_breakdown: [
    {
      local_date: "2026-11-13",
      xp: 40,
      lessons_completed: 2,
      daily_goal_met: true,
    },
    {
      local_date: "2026-11-12",
      xp: 30,
      lessons_completed: 2,
      daily_goal_met: true,
    },
  ],
  streak_breakdown: {
    current_streak: 7,
    longest_streak: 23,
    freezes_held: 1,
    freezes_used: 1,
    recent_protections: [
      {
        protected_on: "2026-11-05",
        remaining: 1,
        streak_length: 7,
      },
    ],
  },
  completion_breakdown: {
    total_completions: 12,
    perfect_completions: 6,
    total_duration_ms: 720000,
    average_duration_ms: 60000,
    first_completed_at: "2026-11-01T12:00:00Z",
    last_completed_at: "2026-11-13T08:00:00Z",
  },
};

for (const mobile of [false, true]) {
  test(`compare Learn, Plus, and Pro analytics and export permissions ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });

    let currentTier: "free" | "learn" | "plus" | "pro" | "expired" = "plus";
    let exportHistoryCalled = false;
    let exportAnalyticsCalled = false;
    let offline = false;

    await page.route("**/v1/gamification/analytics", async (route) => {
      if (offline) {
        return route.abort("failed");
      }
      if (currentTier === "free") {
        return route.fulfill({
          status: 403,
          contentType: "application/json",
          body: JSON.stringify({
            error:
              "Advanced learning analytics requires an active Plus or Pro subscription.",
            code: "subscription_required",
          }),
        });
      }
      if (currentTier === "expired") {
        return route.fulfill({
          status: 403,
          contentType: "application/json",
          body: JSON.stringify({
            error: "Subscription expired. Renew to access learning analytics.",
            code: "subscription_expired",
          }),
        });
      }
      if (currentTier === "learn") {
        return route.fulfill({
          status: 403,
          contentType: "application/json",
          body: JSON.stringify({
            error:
              "Advanced learning analytics requires an active Plus or Pro subscription.",
            code: "subscription_required",
            upsell_suppressed: true,
          }),
        });
      }
      const isPro = currentTier === "pro";
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          ...sampleAnalytics,
          plan_id: isPro ? "pro_monthly" : "plus_monthly",
          plan_name: isPro ? "Pro" : "Plus",
          custom_export_allowed: isPro,
        }),
      });
    });

    await page.route("**/v1/gamification/export/history**", async (route) => {
      exportHistoryCalled = true;
      if (currentTier === "free" || currentTier === "expired") {
        return route.fulfill({
          status: 403,
          contentType: "application/json",
          body: JSON.stringify({
            error:
              "Learning history export requires an active Learn, Plus, or Pro subscription.",
          }),
        });
      }
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        headers: {
          "Content-Disposition":
            'attachment; filename="duolify-learning-history.json"',
          "Cache-Control": "no-store",
        },
        body: JSON.stringify({
          schema_version: 1,
          exported_at: "2026-11-13T13:30:00Z",
          plan_id: currentTier + "_monthly",
          history: [
            {
              lesson_id: "l1",
              course_id: "c1",
              course_title: "Astronomy Basics",
              xp: 15,
              perfect: true,
              earned_at: "2026-11-13T08:00:00Z",
              local_date: "2026-11-13",
              time_zone: "Asia/Ho_Chi_Minh",
              duration_ms: 60000,
            },
          ],
        }),
      });
    });

    await page.route("**/v1/gamification/export/analytics**", async (route) => {
      exportAnalyticsCalled = true;
      if (currentTier !== "pro") {
        return route.fulfill({
          status: 403,
          contentType: "application/json",
          body: JSON.stringify({
            error:
              "Custom analytics export requires an active Pro subscription.",
          }),
        });
      }
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        headers: {
          "Content-Disposition":
            'attachment; filename="duolify-analytics-export.json"',
          "Cache-Control": "no-store",
        },
        body: JSON.stringify({
          schema_version: 1,
          exported_at: "2026-11-13T13:30:00Z",
          dimensions: ["course", "daily"],
          data: { course: [], daily: [] },
        }),
      });
    });

    // 1. Plus tier: view advanced analytics and export history
    currentTier = "plus";
    await page.goto("/analytics");

    const section = page.getByRole("region", { name: "Learning analytics" });
    await expect(section).toBeVisible();

    // Verify course breakdown
    await expect(section.getByText("Astronomy Basics")).toBeVisible();
    await expect(section.getByText("Plant Care")).toBeVisible();

    // Verify stats
    await expect(section.getByText("240")).toBeVisible(); // total xp
    await expect(section.getByText("7")).toBeVisible(); // current streak

    // Export learning history button is visible and active
    const exportHistoryBtn = section.getByRole("button", {
      name: /export learning history/i,
    });
    await expect(exportHistoryBtn).toBeVisible();

    if (mobile) {
      await exportHistoryBtn.click();
    } else {
      await exportHistoryBtn.focus();
      await exportHistoryBtn.press("Enter");
    }
    await expect.poll(() => exportHistoryCalled).toBe(true);

    // Custom analytics export indicates Pro is required for Plus subscribers
    await expect(
      section.getByText(/custom export is part of pro/i),
    ).toBeVisible();
    // Plus includes "No upsell banners": nothing here asks to upgrade.
    await expect(section.getByText(/upgrade/i)).toHaveCount(0);

    // 2. Pro tier: custom analytics dimensions export enabled
    currentTier = "pro";
    await page.goto("/analytics");
    const proSection = page.getByRole("region", {
      name: "Learning analytics",
    });
    await expect(proSection).toBeVisible();

    const customExportBtn = proSection.getByRole("button", {
      name: /export custom analytics/i,
    });
    await expect(customExportBtn).toBeVisible();

    if (mobile) {
      await customExportBtn.click();
    } else {
      await customExportBtn.focus();
      await customExportBtn.press("Enter");
    }
    await expect.poll(() => exportAnalyticsCalled).toBe(true);

    // 3. Learn tier: history export allowed, upsell for advanced analytics
    currentTier = "learn";
    await page.goto("/analytics");
    const learnSection = page.getByRole("region", {
      name: "Learning analytics",
    });
    await expect(learnSection).toBeVisible();
    await expect(
      learnSection.getByText(/not included in your plan/i),
    ).toBeVisible();
    await expect(
      learnSection.getByRole("link", { name: /view plans/i }),
    ).toHaveCount(0);
    await expect(
      learnSection.getByRole("button", { name: /export learning history/i }),
    ).toBeVisible();

    // 4. Free tier: upsell banner, no private text, link to credits
    currentTier = "free";
    await page.goto("/analytics");
    const freeSection = page.getByRole("region", {
      name: "Learning analytics",
    });
    await expect(freeSection).toBeVisible();
    await expect(
      freeSection.getByText(/plus or pro subscription required/i),
    ).toBeVisible();
    await expect(
      freeSection.getByRole("link", { name: /view plans/i }),
    ).toBeVisible();
    await expect(
      freeSection.getByRole("button", { name: /export learning history/i }),
    ).toHaveCount(0);

    // 5. Expired tier: renewal guidance
    currentTier = "expired";
    await page.goto("/analytics");
    const expiredSection = page.getByRole("region", {
      name: "Learning analytics",
    });
    await expect(expiredSection).toBeVisible();
    await expect(
      expiredSection.getByRole("heading", { name: /subscription expired/i }),
    ).toBeVisible();

    // 6. Offline / Error recovery
    offline = true;
    currentTier = "plus";
    await page.goto("/analytics");
    const errorAlert = page.getByRole("alert");
    await expect(errorAlert).toBeVisible();
    const retryBtn = errorAlert.getByRole("button", { name: /retry/i });
    await expect(retryBtn).toBeVisible();

    offline = false;
    await retryBtn.click();
    await expect(
      page.getByRole("region", { name: "Learning analytics" }),
    ).toBeVisible();
  });
}
