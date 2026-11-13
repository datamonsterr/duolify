import { test, expect } from "./accounts-fixture";

for (const mobile of [false, true]) {
  test(`create confirmed edited outline and reload ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });
    let creates = 0;
    let draft = {
      id: "draft",
      brief_revision: "brief",
      revision: 2,
      outline: [
        {
          id: "u2",
          title: "Applied chat title",
          lessons: [{ id: "l2", title: "Planets", goal: "Observe planets" }],
        },
        {
          id: "u1",
          title: "Moon",
          lessons: [{ id: "l1", title: "Phases", goal: "Explain phases" }],
        },
      ],
      history: [],
      confirmed: null,
    };
    await page.route("**/v1/course-brief/config", (r) =>
      r.fulfill({
        json: { enabled: true, credits: 0, pricing_version: "fixture" },
      }),
    );
    await page.route("**/v1/course-brief/confirm", (r) =>
      r.fulfill({ json: { id: "brief", brief: { topic: "Sky" } } }),
    );
    await page.route("**/v1/outline-drafts?*", (r) =>
      r.fulfill({ json: { draft } }),
    );
    await page.route("**/v1/outline-drafts/draft", (r) => {
      if (r.request().method() === "PUT")
        draft = {
          ...draft,
          ...r.request().postDataJSON(),
          revision: draft.revision + 1,
        };
      return r.fulfill({ json: draft });
    });
    await page.route("**/v1/outline-drafts/draft/confirm", (r) =>
      r.fulfill({
        json: {
          draft_id: draft.id,
          revision: draft.revision,
          content_hash: "hash",
          outline: draft.outline,
        },
      }),
    );
    await page.route("**/v1/me/credits", (r) =>
      r.fulfill({ json: { purchased: 400, subscription: 0 } }),
    );
    await page.route("**/v1/course-creations/quote", (r) =>
      r.fulfill({
        json: {
          id: "quote",
          content_ceiling: 55,
          audio_ceiling: 0,
          pricing_version: "fixture",
          expires_at: new Date(Date.now() + 300000).toISOString(),
        },
      }),
    );
    await page.route("**/v1/quotes/quote/accept", (r) =>
      r.fulfill({ json: { accepted: true } }),
    );
    await page.route("**/v1/course-creations", (r) => {
      creates++;
      expect(r.request().postDataJSON()).toEqual({
        draft_id: "draft",
        expected_revision: draft.revision,
        quote_id: "quote",
      });
      return r.fulfill({
        json: {
          id: "creation",
          state: "enrolled",
          course_id: "c2222222-2222-4222-8222-222222222222",
        },
      });
    });
    await page.route(
      "**/v1/courses/c2222222-2222-4222-8222-222222222222",
      (r) =>
        r.fulfill({
          json: {
            id: "c2222222-2222-4222-8222-222222222222",
            title: "Sky",
            description: "",
            units: draft.outline.map((u, i) => ({
              ...u,
              lessons: u.lessons.map((l) => ({
                ...l,
                state: i === 0 ? "ready_to_generate" : "locked",
              })),
            })),
          },
        }),
    );
    await page.goto("/courses/create");
    await page.getByRole("link", { name: "Prefer a form?" }).click();
    await page.getByLabel("Topic", { exact: true }).fill("Sky");
    await page
      .getByRole("button", { name: "Confirm brief", exact: true })
      .click();
    await page.getByRole("button", { name: "Resume saved outline" }).click();
    await page
      .getByLabel("Title for Moon", { exact: true })
      .fill("My edited Moon");
    await page
      .getByRole("button", { name: "Confirm outline", exact: true })
      .click();
    expect(creates).toBe(0);
    await page.getByRole("button", { name: "Review creation cost" }).click();
    await expect(page.getByText(/Reserve up to 55 credits/)).toBeVisible();
    expect(creates).toBe(0);
    await page.getByRole("button", { name: /Create outline.*55/ }).focus();
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(
      /course\/c2222222-2222-4222-8222-222222222222/,
    );
    await expect(
      page.getByRole("heading", { name: "My edited Moon" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Applied chat title" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: /Ready to generate.*Planets/ }),
    ).toBeDisabled();
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "My edited Moon" }),
    ).toBeVisible();
    expect(creates).toBe(1);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}
