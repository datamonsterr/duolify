import { expect, test } from "@playwright/test";
import { apiBase, resetAuthRateLimits, webBase } from "./servers";
import type { Tokens } from "../src/features/accounts/secure-store";

test.use({ hasTouch: true });

for (const width of [1280, 390]) {
  test(`E9-03 exhaust credits, grant next day and enrich oldest notes first at ${width}px`, async ({
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

    // 1. Authenticate a new learner
    const email = `notes-e903-${width}-${Date.now()}@example.test`;
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

    const authHeaders = { Authorization: `Bearer ${tokens.access_token}` };

    // 2. Exhaust credits (simulate today's grant exhausted with zero balance)
    const exhaustRes = await request.post(`${apiBase}/test/credits/exhaust`, {
      headers: authHeaders,
    });
    expect(exhaustRes.ok()).toBeTruthy();

    // 3. Open lesson player and create Note 1 (Oldest note)
    await page.goto(`${webBase}/lesson/sky`);
    await expect(page.getByText(/Why the sky changes/i)).toBeVisible();

    const quickNoteBtn = page.getByRole("button", {
      name: "Quick note",
      exact: true,
    });
    await expect(quickNoteBtn).toBeVisible();
    await quickNoteBtn.click();

    // Check textarea focus and fill Note 1
    const textarea = page.getByRole("textbox", { name: /Your note/i });
    await expect(textarea).toBeVisible();
    const noteText1 =
      'Oldest note: Stars "move" because Earth spins west to east.';
    await textarea.fill(noteText1);

    // Save Note 1
    const saveBtn = page.getByRole("button", { name: /Save quick note/i });
    await saveBtn.click();

    // Verify S09 canvas banner: Saved. Enrichment is pending. and zero-credit variant
    await expect(
      page.getByText(/Saved\. Enrichment is pending\./i),
    ).toBeVisible();
    await expect(page.getByText(/Zero-credit variant/i)).toBeVisible();

    // Close panel
    const closeBtn = page.getByRole("button", { name: "Close Quick note" });
    if (await closeBtn.isVisible()) {
      await closeBtn.click();
    } else {
      await page.getByRole("button", { name: "Back to lesson" }).click();
    }

    // Advance to Slide 2
    const nextBtn = page.getByRole("button", { name: "Next" });
    await expect(nextBtn).toBeVisible();
    await nextBtn.click();
    await expect(page.getByText(/Slide 2 of 3/i)).toBeVisible();

    // 4. Create Note 2 (Newer note)
    await quickNoteBtn.click();
    const textarea2 = page.getByRole("textbox", { name: /Your note/i });
    await expect(textarea2).toBeVisible();
    await textarea2.fill("Newer note: Polaris stays still near the pole.");
    const saveBtn2 = page.getByRole("button", { name: /Save quick note/i });
    await saveBtn2.click();
    await expect(
      page.getByText(/Saved\. Enrichment is pending\./i),
    ).toBeVisible();

    // 5. Navigate to Notes page
    await page.goto(`${webBase}/notes`);
    await expect(
      page.getByRole("heading", { name: /Quick notes/i }),
    ).toBeVisible();

    // Both notes appear in the list with "Enrichment pending"
    await expect(page.getByText(noteText1).first()).toBeVisible();
    await expect(page.getByText(/Newer note/i).first()).toBeVisible();
    await expect(page.getByText(/Enrichment pending/i).first()).toBeVisible();

    // 6. Grant credits (e.g. next day daily grant arrives)
    const grantRes = await request.post(`${apiBase}/test/credits/next-day`, {
      headers: authHeaders,
    });
    expect(grantRes.ok()).toBeTruthy();

    // Wait for background River job to pick up and enrich the oldest note
    await expect
      .poll(
        async () => {
          const res = await request.get(`${apiBase}/v1/notes`, {
            headers: authHeaders,
          });
          if (!res.ok()) return "";
          const data = await res.json();
          const list = Array.isArray(data) ? data : (data.notes ?? []);
          const older = (
            list as Array<{ raw_text?: string; status?: string }>
          ).find((n) => n.raw_text?.includes("Oldest note"));
          return older?.status ?? "";
        },
        { timeout: 15000, intervals: [500, 1000] },
      )
      .toBe("enriched");

    // 7. Verify pending enrichment resumes automatically, oldest note first!
    await page.reload();
    await expect(
      page.getByRole("heading", { name: /Quick notes/i }),
    ).toBeVisible();

    if (width === 390) {
      await page.getByText(noteText1).first().click();
      await expect(
        page.getByRole("button", { name: /Back to notes/i }),
      ).toBeVisible();
    } else {
      await page.getByText(noteText1).first().click();
    }

    await expect(page.getByText(/Summary:/i)).toBeVisible();
    await expect(page.getByText(/AI additions/i)).toBeVisible();
    await expect(page.getByText(/#rotation|#polaris/i).first()).toBeVisible();
  });
}
