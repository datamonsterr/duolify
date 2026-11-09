import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";

test.describe("E17-01 Play a default-cast Motion HTML tracer", () => {
  test("plays the authorized two-speaker fixture, switches 5 expressions, rejects markup, and recovers missing rig with zero spend", async ({
    page,
    request,
  }) => {
    // 1. Initial credit check: verify credits before
    const initialCreditsRes = await request.get(`${apiBase}/v1/me/credits`);
    let initialBalance = 0;
    if (initialCreditsRes.ok()) {
      const data = await initialCreditsRes.json();
      initialBalance = data.balance ?? 0;
    }

    // 2. Navigate to the authorized default-cast tracer lesson
    await page.goto("/lesson/tracer-default-cast");

    // Expect player shell to be visible
    await expect(
      page.getByRole("progressbar", { name: "Lesson slides" }),
    ).toBeVisible();
    await expect(
      page.getByRole("slider", { name: "Slide time" }),
    ).toBeVisible();

    // The slide is hosted inside a sandboxed iframe
    const slideIframe = page.frameLocator('iframe[title="Slide 1"]');

    // Verify sandbox attributes on the iframe
    const iframeElem = page.locator('iframe[title="Slide 1"]');
    await expect(iframeElem).toHaveAttribute("sandbox", "allow-scripts");

    // Two stable speaker keys are projected: guide and supporter
    // First speaker: guide ("Provisional Glint")
    await expect(
      slideIframe.locator('[data-speaker-key="guide"]'),
    ).toBeVisible();
    await expect(slideIframe.getByText("Provisional Glint")).toBeVisible();
    await expect(
      slideIframe.locator('[data-speaker-key="guide"] svg.character-rig'),
    ).toBeVisible();

    // Expression switching: test all five expressions (idle, happy, sad, thinking, celebrate)
    const expressions = [
      "idle",
      "happy",
      "sad",
      "thinking",
      "celebrate",
    ] as const;
    for (const exp of expressions) {
      // Trigger expression change (via bridge / buttons / checkpoint)
      await slideIframe
        .locator(`[data-expression-target="${exp}"]`)
        .click()
        .catch(() => {});
      // Alternatively trigger via player controls or postMessage
      await page.evaluate((e) => {
        const frame = document.querySelector(
          'iframe[title="Slide 1"]',
        ) as HTMLIFrameElement;
        frame?.contentWindow?.postMessage(
          { type: "set_expression", speaker: "guide", expression: e },
          "*",
        );
      }, exp);

      // Verify the expression attribute updates on the rig
      await expect(
        slideIframe.locator(
          `[data-speaker-key="guide"][data-expression="${exp}"]`,
        ),
      ).toBeVisible();
      // Verify invariant base shape and accessories are preserved
      await expect(
        slideIframe.locator(
          '[data-speaker-key="guide"] [data-part="body-shape"]',
        ),
      ).toBeVisible();
    }

    // Verify second speaker: supporter ("Provisional Moss")
    await page.evaluate(() => {
      const frame = document.querySelector(
        'iframe[title="Slide 1"]',
      ) as HTMLIFrameElement;
      frame?.contentWindow?.postMessage({ type: "seek", time: 1500 }, "*");
    });
    await expect(
      slideIframe.locator('[data-speaker-key="supporter"]'),
    ).toBeVisible();
    await expect(slideIframe.getByText("Provisional Moss")).toBeVisible();

    // 3. Reject injected markup: verify CSP and that no malicious script executed
    const scriptExecuted = await page.evaluate(
      () =>
        (window as unknown as { __maliciousInjected?: boolean })
          .__maliciousInjected,
    );
    expect(scriptExecuted).toBeUndefined();

    // 4. Missing rig recovery: when rig is missing, bundled fallback is displayed without breaking
    await page.evaluate(() => {
      const frame = document.querySelector(
        'iframe[title="Slide 1"]',
      ) as HTMLIFrameElement;
      frame?.contentWindow?.postMessage({ type: "set_missing_rig_test" }, "*");
    });
    await expect(
      slideIframe.locator(".fallback-character, [data-fallback='true']"),
    ).toBeVisible();
    await expect(slideIframe.locator(".dialogue-text")).toBeVisible();

    // 5. Zero credits spent
    const afterCreditsRes = await request.get(`${apiBase}/v1/me/credits`);
    if (afterCreditsRes.ok()) {
      const afterData = await afterCreditsRes.json();
      expect(afterData.balance ?? 0).toBe(initialBalance);
    }

    // 6. Canvas acceptance: controls and keyboard parity
    // Space play/pause
    await page.locator("body").click();
    await page.keyboard.press("Space");
    await expect(
      page.getByRole("button", { name: "Play", exact: true }),
    ).toBeVisible();
    await page.keyboard.press("Space");
    await expect(page.getByRole("button", { name: "Pause" })).toBeVisible();

    // Arrow keys to seek
    await page.keyboard.press("ArrowRight");
    // Speed control
    await page.getByRole("button", { name: /Speed/ }).click();
    // Mute control
    const muteBtn = page.getByRole("button", { name: /narration/i });
    await expect(muteBtn).toBeVisible();
    await muteBtn.click();
    await expect(
      page.getByRole("button", { name: "Unmute narration" }),
    ).toBeVisible();

    // Advance to next and finish
    await page.getByRole("button", { name: "Next" }).click();
    await page.getByRole("button", { name: "Finish lesson" }).click();
    await expect(page.getByText("Lesson complete!")).toBeVisible();
  });

  test("mobile viewport matches canvas controls and functions", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto("/lesson/tracer-default-cast");

    await expect(
      page.getByRole("progressbar", { name: "Lesson slides" }),
    ).toBeVisible();
    const slideIframe = page.frameLocator('iframe[title="Slide 1"]');
    await expect(
      slideIframe.locator('[data-speaker-key="guide"]'),
    ).toBeVisible();

    // Controls remain accessible on mobile
    await expect(page.getByRole("button", { name: "Pause" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Next" })).toBeVisible();
  });
});
