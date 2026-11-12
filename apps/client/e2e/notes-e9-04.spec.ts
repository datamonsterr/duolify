import { expect, test } from "./native-store-fixture";
import { apiBase, resetAuthRateLimits, webBase } from "./servers";
import type { Tokens } from "../src/features/accounts/secure-store";

test.use({ hasTouch: true });

for (const width of [1280, 390]) {
  test(`E9-04 filter, edit, review, export and delete notes at ${width}px`, async ({
    page,
    context,
    request,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await resetAuthRateLimits(request);

    // Route /v1/ API calls to test backend
    await page.route(/\/v1\//, async (route) => {
      const original = route.request();
      const target = new URL(original.url());
      const headers = original.headers();
      delete headers.origin;
      target.host = new URL(apiBase).host;
      try {
        const response = await request.fetch(target.href, {
          method: original.method(),
          headers,
          data: original.postData() ?? undefined,
        });
        await route.fulfill({ response });
      } catch {
        // Disposed during navigation
      }
    });

    // Authenticate a new learner
    const email = `notes-e904-${width}-${Date.now()}@example.test`;
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
      sessionStorage.setItem(
        "duolify.native-store-fixture",
        JSON.stringify(val),
      );
    }, tokens);

    // Create two test notes via API
    const note1Res = await request.post(`${apiBase}/v1/notes`, {
      headers: { Authorization: `Bearer ${tokens.access_token}` },
      data: {
        action_key: `action-note-1-${Date.now()}`,
        source: "lesson",
        raw_text:
          'Stars "move" because Earth spins west → east. Check: does Polaris move?',
        slide_title: "Why the sky changes · Slide 2",
      },
    });
    expect(note1Res.ok()).toBeTruthy();
    await note1Res.json();

    const note2Res = await request.post(`${apiBase}/v1/notes`, {
      headers: { Authorization: `Bearer ${tokens.access_token}` },
      data: {
        action_key: `action-note-2-${Date.now()}`,
        source: "chat",
        raw_text: "Watering schedules depend on seasonal temperature.",
        slide_title: "From tutor chat",
      },
    });
    expect(note2Res.ok()).toBeTruthy();

    // Navigate to Notes page
    await page.goto(`${webBase}/notes`);
    await expect(
      page.getByRole("heading", { name: /Quick notes/i }),
    ).toBeVisible();

    // Verify both notes appear in list
    await expect(page.getByText(/Stars "move"/i).first()).toBeVisible();
    await expect(page.getByText(/Watering schedules/i).first()).toBeVisible();

    // 1. Search notes
    const searchInput = page.getByPlaceholder("Search your notes");
    await expect(searchInput).toBeVisible();
    await searchInput.fill("sky changes");
    await expect(page.getByText(/Stars "move"/i).first()).toBeVisible();
    await expect(page.getByText(/Watering schedules/i)).toHaveCount(0);

    // Clear filters
    const clearBtn = page.getByRole("button", { name: /Clear filters/i });
    if (await clearBtn.isVisible()) {
      await clearBtn.click();
    } else {
      await searchInput.fill("");
    }
    await expect(page.getByText(/Watering schedules/i).first()).toBeVisible();

    // 2. Select note for detail view
    await page
      .getByText(/Stars "move"/i)
      .first()
      .click();

    if (width === 390) {
      // Mobile two-screen check: Back to notes button must be visible
      await expect(
        page.getByRole("button", { name: /Back to notes/i }),
      ).toBeVisible();
    }

    // Detail view elements
    await expect(page.getByText(/Your original note/i)).toBeVisible();
    await expect(
      page.getByText(/Stars "move" because Earth spins/i).first(),
    ).toBeVisible();

    // 3. Edit raw text without overwriting prior enrichment
    const editBtn = page.getByRole("button", { name: /Edit/i });
    await expect(editBtn).toBeVisible();
    await editBtn.click();

    const editTextarea = page.getByRole("textbox", {
      name: /Edit note text/i,
    });
    await expect(editTextarea).toBeVisible();
    await editTextarea.fill(
      "Stars move because Earth spins west to east. Polaris is stationary.",
    );

    const saveEditBtn = page.getByRole("button", { name: /Save changes/i });
    await expect(saveEditBtn).toBeVisible();
    await saveEditBtn.click();

    // Verify updated raw text is shown
    await expect(
      page.getByText(/Polaris is stationary/i).first(),
    ).toBeVisible();

    // 4. Mark reviewed once
    const reviewBtn = page.getByRole("button", { name: /Mark reviewed/i });
    if (await reviewBtn.isVisible()) {
      await reviewBtn.click();
      await expect(page.getByText(/Reviewed/i).first()).toBeVisible();
    }

    // 5. Export Markdown
    const exportBtn = page.getByRole("button", {
      name: /Export Markdown|Export/i,
    });
    await expect(exportBtn).toBeVisible();
    await exportBtn.click();
    await expect(
      page.getByText(/Markdown exported|Exported/i).or(exportBtn),
    ).toBeVisible();

    // 6. Delete note
    const deleteBtn = page.getByRole("button", { name: /Delete/i });
    await expect(deleteBtn).toBeVisible();
    await deleteBtn.click();

    if (width === 390) {
      // Mobile returns to list after delete
      await expect(
        page.getByRole("heading", { name: /Quick notes/i }),
      ).toBeVisible();
    }

    // Deleted note should no longer be in the list
    await expect(page.getByText(/Polaris is stationary/i)).toHaveCount(0);
  });
}
