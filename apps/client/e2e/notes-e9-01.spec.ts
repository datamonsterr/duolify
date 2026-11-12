import { expect, test } from "./native-store-fixture";
import { apiBase, resetAuthRateLimits, webBase } from "./servers";
import type { Tokens } from "../src/features/accounts/secure-store";

test.use({ hasTouch: true });

for (const width of [1280, 390]) {
  test(`E9-01 save a zero-credit note offline then synchronize it once at ${width}px`, async ({
    page,
    context,
    request,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await resetAuthRateLimits(request);

    // Route /v1/ API calls to test backend
    let offline = false;
    let demoLesson = "";
    await page.route(/\/v1\//, async (route) => {
      const original = route.request();
      if (offline && original.url().includes("/v1/notes")) {
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
        // Disposed or aborted during navigation
      }
    });

    // Authenticate a new learner
    const email = `notes-${width}-${Date.now()}@example.test`;
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

    const fixtureRes = await request.post(`${apiBase}/test/player/fixture`, {
      headers: { Authorization: `Bearer ${tokens.access_token}` },
    });
    expect(fixtureRes.ok()).toBe(true);
    demoLesson = (await fixtureRes.json()).lesson_id;

    // Ensure learner is in zero-credit state per test specification
    await request.post(`${apiBase}/test/credits/exhaust`, {
      headers: { Authorization: `Bearer ${tokens.access_token}` },
    });

    await context.addInitScript((val) => {
      sessionStorage.setItem(
        "duolify.native-store-fixture",
        JSON.stringify(val),
      );
    }, tokens);

    // Navigate to lesson player
    await page.goto(`${webBase}/lesson/${demoLesson}`);
    await expect(page.getByText(/Why the sky changes/i)).toBeVisible();

    // Trigger Quick note panel/sheet
    const quickNoteBtn = page.getByRole("button", {
      name: "Quick note",
      exact: true,
    });
    await expect(quickNoteBtn).toBeVisible();
    await quickNoteBtn.click();

    // Verify source context chip per canvas
    await expect(
      page.getByText(/Slide 1 · Why the sky changes/i),
    ).toBeVisible();

    // Textarea input
    const textarea = page.getByRole("textbox", { name: /Your note/i });
    await expect(textarea).toBeVisible();
    await expect(textarea).toBeFocused();

    const noteText =
      'Stars "move" because Earth spins west → east. Check: does Polaris move?';
    await textarea.fill(noteText);

    // Verify character count helper text (71 / 2,000)
    await expect(
      page.getByText(new RegExp(`${noteText.length} \\/ 2,000`)),
    ).toBeVisible();

    // Go offline for notes API
    offline = true;

    // Save note offline
    const saveBtn = page.getByRole("button", { name: /Save quick note/i });
    await expect(saveBtn).toBeEnabled();
    await saveBtn.click();

    // Verify offline save banner
    await expect(
      page.getByText(/Offline variant · saved on this device/i),
    ).toBeVisible();
    await expect(
      page.getByText(/It will sync once you're online. Not backed up yet./i),
    ).toBeVisible();

    // Close the panel/sheet to verify player focus and position restoration
    const closeBtn = page.getByRole("button", { name: "Close Quick note" });
    if (await closeBtn.isVisible()) {
      await closeBtn.click();
    } else {
      await page.getByRole("button", { name: "Back to lesson" }).click();
    }
    await expect(quickNoteBtn).toBeFocused();

    // Reopen quick note sheet
    await quickNoteBtn.click();

    // Restore online connectivity and synchronize once
    offline = false;
    const syncBtn = page.getByRole("button", {
      name: /Sync now|Synchronize|Retry sync/i,
    });
    await expect(syncBtn).toBeVisible();
    await syncBtn.click();

    // Verify synced banner and zero-credit status
    await expect(
      page.getByText(/Saved. Enrichment is pending./i),
    ).toBeVisible();
    await expect(
      page.getByText(
        /Synced to your account. The AI summary and tags appear in Quick notes./i,
      ),
    ).toBeVisible();
    await expect(page.getByText(/Zero-credit variant/i)).toBeVisible();
    await expect(
      page.getByText(
        /Your note is saved. Add credits to enrich it, or wait for your daily reset./i,
      ),
    ).toBeVisible();

    // Navigate to /notes to view notes list
    await page.goto(`${webBase}/notes`);
    await expect(
      page.getByRole("heading", { name: /Quick notes/i }),
    ).toBeVisible();
    await expect(page.getByText(noteText).first()).toBeVisible();
    await expect(
      page.getByText("Enrichment pending", { exact: true }).first(),
    ).toBeVisible();
  });
}
