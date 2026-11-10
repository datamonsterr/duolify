import { expect, test } from "@playwright/test";
import { apiBase, resetAuthRateLimits, webBase } from "./servers";
import type { Tokens } from "../src/features/accounts/secure-store";

test.use({ hasTouch: true });

for (const width of [1280, 390]) {
  test(`E9-02 save a note and inspect enriched output with a hallucinated link rejected at ${width}px`, async ({
    page,
    context,
    request,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await resetAuthRateLimits(request);

    let demoLesson: string | undefined;
    await page.route(/\/v1\//, async (route) => {
      const original = route.request();
      const target = new URL(original.url());
      const headers = original.headers();
      delete headers.origin;
      target.host = new URL(apiBase).host;
      const demo = target.pathname.startsWith("/v1/lessons/sky/");
      if (demo) {
        if (!demoLesson) {
          const response = await request.post(
            `${apiBase}/test/player/fixture`,
            { headers: { Authorization: headers.authorization } },
          );
          if (response.ok()) {
            demoLesson = (await response.json()).lesson_id;
          }
        }
        if (demoLesson) {
          target.pathname = target.pathname.replace(
            "/lessons/sky/",
            `/lessons/${demoLesson}/`,
          );
        }
      }
      try {
        const response = await request.fetch(target.href, {
          method: original.method(),
          headers,
          data: original.postData() ?? undefined,
        });
        if (demo && target.pathname.endsWith("/manifest") && response.ok()) {
          const manifest = await response.json();
          manifest.lesson_id = "sky";
          manifest.course_id = "astronomy";
          for (const slide of manifest.slides)
            slide.html_url = slide.html_url.replace(
              `/lessons/${demoLesson}/`,
              "/lessons/sky/",
            );
          await route.fulfill({ json: manifest });
        } else {
          await route.fulfill({ response });
        }
      } catch {
        // Disposed during navigation
      }
    });

    // Authenticate a new learner
    const email = `notes-e902-${width}-${Date.now()}@example.test`;
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

    await context.addInitScript((val) => {
      sessionStorage.setItem("duolify.auth", JSON.stringify(val));
    }, tokens);

    // Navigate to lesson player
    await page.goto(`${webBase}/lesson/sky`);
    await expect(page.getByText(/Why the sky changes/i)).toBeVisible();

    // Trigger Quick note panel
    const quickNoteBtn = page.getByRole("button", {
      name: "Quick note",
      exact: true,
    });
    await expect(quickNoteBtn).toBeVisible();
    await quickNoteBtn.click();

    // Verify slide source context chip
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

    // Verify enrich checkbox exists and is checked
    const enrichCheckbox = page.getByLabel(/Enrich with AI/i);
    await expect(enrichCheckbox).toBeVisible();
    await expect(enrichCheckbox).toBeChecked();

    // Save note
    const saveBtn = page.getByRole("button", { name: /Save quick note/i });
    await expect(saveBtn).toBeEnabled();
    await saveBtn.click();

    // Close panel
    const closeBtn = page.getByRole("button", { name: "Close Quick note" });
    if (await closeBtn.isVisible()) {
      await closeBtn.click();
    } else {
      await page.getByRole("button", { name: "Back to lesson" }).click();
    }

    // Navigate to Notes page
    await page.goto(`${webBase}/notes`);
    await expect(
      page.getByRole("heading", { name: /Quick notes/i }),
    ).toBeVisible();

    // The note must appear in the list
    await expect(page.getByText(noteText).first()).toBeVisible();

    // If mobile, click note card to open detail view
    if (width === 390) {
      await page.getByText(noteText).first().click();
      await expect(
        page.getByRole("button", { name: /Back to notes/i }),
      ).toBeVisible();
    }

    // Check separation of original note and AI additions
    await expect(page.getByText(/Your original note/i)).toBeVisible();
    await expect(page.getByText(/AI additions/i)).toBeVisible();

    // Summary must be present
    await expect(page.getByText(/Summary:/i)).toBeVisible();

    // Tags must be present (#rotation or #polaris)
    await expect(page.getByText(/#rotation|#polaris/i).first()).toBeVisible();

    // Valid lesson link must be displayed (Related: ... or lesson link)
    await expect(page.getByText(/Related:/i)).toBeVisible();

    // Hallucinated / foreign link (e.g. unknown-foreign-lesson) must NOT be displayed
    await expect(page.getByText(/unknown-foreign-lesson/i)).toHaveCount(0);
  });
}
