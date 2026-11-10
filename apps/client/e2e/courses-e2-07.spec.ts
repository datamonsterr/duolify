import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";
import type { OutlineDraft } from "../src/features/courses/outline";

for (const mobile of [false, true]) {
  test(`outline editor persists edits and undo ${mobile ? "mobile" : "desktop"}`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });
    let paidCalls = 0;
    let failNextSave = false;
    let saved: OutlineDraft | undefined;
    await page.route(/\/v1\/(?:course-brief|outline-drafts)/, async (route) => {
      const original = route.request();
      const target = new URL(original.url());
      target.host = new URL(apiBase).host;
      if (
        failNextSave &&
        target.pathname === "/v1/outline-drafts" &&
        original.method() === "POST"
      ) {
        failNextSave = false;
        await route.fulfill({
          status: 503,
          json: { error: "Offline. Your edits are kept." },
        });
        return;
      }
      const headers = original.headers();
      delete headers.origin;
      const response = await request.fetch(target.href, {
        method: original.method(),
        headers,
        data: original.postData() ?? undefined,
      });
      if (
        ["POST", "PUT"].includes(original.method()) &&
        target.pathname.startsWith("/v1/outline-drafts") &&
        !target.pathname.endsWith("/confirm") &&
        response.ok()
      )
        saved = (await response.json()) as OutlineDraft;
      await route.fulfill({ response });
    });
    await page.route("**/v1/me/credits", (route) =>
      route.fulfill({ json: { purchased: 400, subscription: 0 } }),
    );
    await page.route("**/v1/quotes", (route) =>
      route.fulfill({
        json: {
          id: "preview-quote",
          content_ceiling: 25,
          pricing_version: "fixture",
          expires_at: new Date(Date.now() + 300000).toISOString(),
        },
      }),
    );
    await page.route("**/v1/quotes/preview-quote/accept", (route) =>
      route.fulfill({ json: {} }),
    );
    await page.route("**/v1/courses/preview", (route) => {
      paidCalls++;
      return route.fulfill({
        json: {
          sample_outline: {
            title: "Sky",
            units: [
              {
                title: "Moon",
                lessons: [
                  { title: "Phases", goal: "Recognize phases" },
                  { title: "Tides", goal: "Explain tides" },
                ],
              },
              {
                title: "Stars",
                lessons: [{ title: "Map", goal: "Read a map" }],
              },
            ],
          },
          example_slides: [
            {
              slide_id: "sample",
              html: '<div id="root">Sky</div>',
              narration: "Look up",
              duration_ms: 1000,
            },
          ],
        },
      });
    });
    await page.goto("/courses/create");
    await page.getByRole("link", { name: "Prefer a form?" }).click();
    await page.getByLabel("Topic", { exact: true }).fill("Sky");
    await page
      .getByRole("button", { name: "Confirm brief", exact: true })
      .click();
    await page.getByRole("button", { name: "Review preview cost" }).click();
    await page.getByRole("button", { name: /Confirm & preview/ }).click();
    await page.getByRole("button", { name: "Review sample outline" }).click();
    await expect(
      page.getByRole("heading", { name: "Shape your outline" }),
    ).toBeVisible();
    const move = page.getByRole("button", {
      name: "Move Stars up",
      exact: true,
    });
    await move.focus();
    await page.keyboard.press("Enter");
    await expect(move).toBeFocused();
    await expect(
      page.getByRole("status").filter({ hasText: "Stars moved to position 1" }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Move Tides up", exact: true })
      .click();
    if (!mobile) {
      await page
        .getByRole("button", { name: "Reorder Stars", exact: true })
        .dragTo(
          page.getByRole("button", { name: "Reorder Moon", exact: true }),
        );
    }
    await page
      .getByLabel("Title for Phases", { exact: true })
      .fill("Moon phases");
    await page
      .getByRole("button", { name: "Remove Tides", exact: true })
      .click();
    await page.getByRole("button", { name: "Add a unit", exact: true }).click();
    await page
      .getByLabel("Title for New unit", { exact: true })
      .fill("Planets");
    await page
      .getByRole("button", { name: "Add a lesson to Planets", exact: true })
      .click();
    await page
      .getByLabel("Goal for New lesson", { exact: true })
      .fill("Recognize a planet");
    await page
      .getByRole("button", { name: "Remove Stars", exact: true })
      .click();
    await expect(
      page.getByLabel("Title for Stars", { exact: true }),
    ).toHaveCount(0);
    await page.getByRole("button", { name: "Undo", exact: true }).click();
    await expect(
      page.getByLabel("Title for Stars", { exact: true }),
    ).toHaveValue("Stars");
    failNextSave = true;
    await page
      .getByRole("button", { name: "Save outline", exact: true })
      .click();
    await expect(page.getByRole("alert")).toContainText("Your edits are kept");
    page.once("dialog", (dialog) => dialog.accept());
    await page.reload();
    await page.getByRole("button", { name: "Resume unsaved outline" }).click();
    await expect(
      page.getByLabel("Goal for New lesson", { exact: true }),
    ).toHaveValue("Recognize a planet");
    await page
      .getByRole("button", { name: "Save outline", exact: true })
      .click();
    await expect(
      page.getByText("Outline saved.", { exact: true }),
    ).toBeVisible();
    const credentials = await page.evaluate(() =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
    );
    const concurrent = await request.put(
      `${apiBase}/v1/outline-drafts/${saved!.id}`,
      {
        headers: { Authorization: `Bearer ${credentials.access_token}` },
        data: {
          expected_revision: saved!.revision,
          outline: saved!.outline.map((unit, index) =>
            index === 0 ? { ...unit, title: "Other device" } : unit,
          ),
          history: saved!.history,
        },
      },
    );
    expect(concurrent.status()).toBe(200);
    await page
      .getByLabel("Title for Planets", { exact: true })
      .fill("My planets");
    await page
      .getByRole("button", { name: "Save outline", exact: true })
      .click();
    await expect(page.getByRole("alert")).toContainText("Both drafts are kept");
    await page
      .getByRole("button", { name: "Continue my recovery copy" })
      .click();
    await expect(
      page.getByText(
        "Selected outline loaded. The other saved draft is kept.",
        { exact: true },
      ),
    ).toBeVisible();
    await page.reload();
    await page.getByRole("button", { name: "Resume saved outline" }).click();
    await expect(
      page.getByLabel("Title for Moon phases", { exact: true }),
    ).toHaveValue("Moon phases");
    await expect(
      page.getByLabel("Goal for New lesson", { exact: true }),
    ).toHaveValue("Recognize a planet");
    await page.getByRole("button", { name: "Undo", exact: true }).click();
    await expect(
      page.getByLabel("Title for Planets", { exact: true }),
    ).toHaveValue("Planets");
    await page.getByRole("button", { name: "Undo", exact: true }).click();
    await expect(
      page.getByLabel("Goal for New lesson", { exact: true }),
    ).toHaveValue("Describe what you learned.");
    await page
      .getByRole("button", { name: "Confirm outline", exact: true })
      .click();
    await expect(
      page.getByText(/Outline confirmed. No course has been created/),
    ).toBeVisible();
    expect(paidCalls).toBe(1);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}
