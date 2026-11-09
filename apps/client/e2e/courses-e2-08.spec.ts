import { test, expect } from "./accounts-fixture";

for (const colorScheme of ["light", "dark"] as const)
  for (const mobile of [false, true]) {
    test(`outline proposal consent, diff and free decisions ${mobile ? "mobile" : "desktop"} ${colorScheme}`, async ({
      page,
    }) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
      );
      await page.emulateMedia({
        colorScheme,
        reducedMotion: "reduce",
      });
      const draft = {
        id: "draft",
        brief_revision: "brief",
        revision: 1,
        outline: [
          {
            id: "u1",
            title: "My manual title",
            lessons: [{ id: "l1", title: "Moon", goal: "Observe" }],
          },
        ],
        history: [],
        confirmed: null,
      };
      let generations = 0;
      let decisions = 0;
      await page.route("**/v1/course-brief/confirm", (route) =>
        route.fulfill({
          json: {
            id: "brief",
            brief: { topic: "Sky" },
            expires_at: new Date(Date.now() + 600000).toISOString(),
          },
        }),
      );
      await page.route("**/v1/outline-drafts?*", (route) =>
        route.fulfill({ json: { draft } }),
      );
      await page.route("**/v1/outline-proposals/quote", (route) =>
        route.fulfill({
          json: {
            id: "quote",
            content_ceiling: 7,
            pricing_version: "unapproved-test",
            daily_free_eligible: true,
            expires_at: new Date(Date.now() + 600000).toISOString(),
          },
        }),
      );
      await page.route("**/v1/quotes/quote/accept", (route) =>
        route.fulfill({ json: { accepted: true } }),
      );
      await page.route("**/v1/outline-proposals", (route) => {
        generations++;
        return route.fulfill({
          json: {
            id: "proposal",
            draft_id: draft.id,
            base_revision: 1,
            state: "delivered",
            decision: "",
            operations: ["added", "removed", "moved", "retitled"].map(
              (kind, i) => ({ kind, id: `node${i}`, title: "Proposed title" }),
            ),
          },
        });
      });
      await page.route("**/v1/outline-proposals/proposal/*", (route) => {
        decisions++;
        const undo = route.request().url().endsWith("/undo");
        return route.fulfill({
          json: {
            draft: {
              ...draft,
              revision: undo ? 3 : 2,
              outline: undo
                ? draft.outline
                : [{ ...draft.outline[0], title: "Applied title" }],
            },
            proposal: {
              id: "proposal",
              state: "delivered",
              decision: undo ? "undone" : "applied",
              operations: [],
            },
          },
        });
      });
      await page.route("**/v1/course-brief/config", (route) =>
        route.fulfill({
          json: { enabled: true, credits: 0, pricing_version: "fixture" },
        }),
      );
      await page.goto("/courses/create");
      await page.getByRole("link", { name: "Prefer a form?" }).click();
      await page.getByLabel("Topic", { exact: true }).fill("Sky");
      await page
        .getByRole("button", { name: "Confirm brief", exact: true })
        .click();
      await page.getByRole("button", { name: "Resume saved outline" }).click();
      if (mobile)
        await page.getByRole("button", { name: "Open outline chat" }).click();
      await page
        .getByLabel("Ask for an outline change", { exact: true })
        .fill("Put planets first");
      await page.getByRole("button", { name: "Review change cost" }).click();
      await expect(page.getByText(/up to 7 credits/i)).toBeVisible();
      expect(generations).toBe(0);
      await page
        .getByRole("button", { name: /Confirm.*suggest change/ })
        .click();
      for (const kind of ["Added", "Removed", "Moved", "Retitled"])
        await expect(page.getByText(kind, { exact: true })).toBeVisible();
      if (mobile)
        await page.getByRole("button", { name: "Close Outline chat" }).click();
      await expect(
        page.getByLabel("Title for My manual title", { exact: true }),
      ).toHaveValue("My manual title");
      if (mobile)
        await page.getByRole("button", { name: "Open outline chat" }).click();
      const apply = page.getByRole("button", { name: "Apply", exact: true });
      await apply.focus();
      await page.keyboard.press("Enter");
      await expect(
        page.getByRole("status").filter({ hasText: "Proposal applied" }),
      ).toBeFocused();
      if (mobile)
        await page.getByRole("button", { name: "Close Outline chat" }).click();
      await expect(
        page.getByLabel("Title for Applied title", { exact: true }),
      ).toBeVisible();
      if (mobile)
        await page.getByRole("button", { name: "Open outline chat" }).click();
      await page
        .getByRole("button", { name: "Undo proposal", exact: true })
        .click();
      await expect(
        page.getByRole("status").filter({ hasText: "Proposal undone" }),
      ).toBeFocused();
      if (mobile)
        await page.getByRole("button", { name: "Close Outline chat" }).click();
      await expect(
        page.getByLabel("Title for My manual title", { exact: true }),
      ).toBeVisible();
      expect(generations).toBe(1);
      expect(decisions).toBe(2);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    });
  }

for (const width of [1280, 320]) {
  test(`outline request recovery and stale diff at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    let draft = {
      id: "recovery-draft",
      brief_revision: "recovery-brief",
      revision: 1,
      outline: [{ id: "u1", title: "My saved title", lessons: [] }],
      history: [],
      confirmed: null,
    };
    const proposal = {
      id: "recovered-proposal",
      draft_id: draft.id,
      base_revision: 1,
      state: "delivered",
      decision: "",
      operations: [
        {
          kind: "retitled",
          id: "u1",
          title: "A very long suggested title ".repeat(15),
        },
      ],
    };
    let quoteMode = "offline";
    let generations = 0;
    await page.route("**/v1/course-brief/config", (route) =>
      route.fulfill({
        json: { enabled: true, credits: 0, pricing_version: "fixture" },
      }),
    );
    await page.route("**/v1/course-brief/confirm", (route) =>
      route.fulfill({
        json: {
          id: "recovery-brief",
          brief: { topic: "Sky" },
          expires_at: new Date(Date.now() + 600000).toISOString(),
        },
      }),
    );
    await page.route("**/v1/outline-drafts?*", (route) =>
      route.fulfill({ json: { draft } }),
    );
    await page.route("**/v1/outline-proposals/quote", (route) =>
      quoteMode === "offline"
        ? route.fulfill({ status: 503, json: { error: "Quote unavailable" } })
        : route.fulfill({
            json: {
              id: "recovery-quote",
              content_ceiling: 11,
              pricing_version: "unapproved-fixture",
              daily_free_eligible: true,
              expires_at: new Date(
                Date.now() + (quoteMode === "expired" ? -10000 : 600000),
              ).toISOString(),
            },
          }),
    );
    await page.route("**/v1/quotes/recovery-quote/accept", (route) =>
      route.fulfill({ json: { accepted: true } }),
    );
    await page.route("**/v1/outline-proposals", (route) => {
      generations++;
      return route.abort("failed");
    });
    await page.route("**/v1/outline-proposals/?quote_id=*", (route) =>
      route.fulfill({ json: proposal }),
    );
    await page.route(
      "**/v1/outline-proposals/recovered-proposal/apply",
      (route) =>
        route.fulfill({
          status: 409,
          json: {
            error:
              "Proposal out of date. Reload the outline and review a fresh quote.",
          },
        }),
    );
    await page.route(
      "**/v1/outline-proposals/recovered-proposal/discard",
      (route) =>
        route.fulfill({
          json: { proposal: { ...proposal, decision: "discarded" }, draft },
        }),
    );
    async function open(resume = false) {
      await page.goto("/courses/create");
      if (!resume) {
        await page.getByRole("link", { name: "Prefer a form?" }).click();
        await page.getByLabel("Topic", { exact: true }).fill("Sky");
        await page
          .getByRole("button", { name: "Confirm brief", exact: true })
          .click();
      }
      await page.getByRole("button", { name: "Resume saved outline" }).click();
      if (width < 768)
        await page.getByRole("button", { name: "Open outline chat" }).click();
    }
    await open();
    const request = "Private request ".repeat(40);
    await expect(
      page.getByRole("button", { name: "Review change cost" }),
    ).toBeDisabled();
    await page.getByLabel("Ask for an outline change").fill(request);
    await page.getByRole("button", { name: "Review change cost" }).click();
    await expect(page.getByRole("alert")).toContainText(
      "Your outline and request are kept",
    );
    quoteMode = "expired";
    await page.getByRole("button", { name: "Review change cost" }).click();
    await expect(
      page.getByText("Quote expired or outline changed. Review a fresh quote."),
    ).toBeVisible();
    expect(generations).toBe(0);
    quoteMode = "ready";
    await page
      .getByRole("button", { name: "Review fresh change cost" })
      .click();
    await page.getByRole("button", { name: /Confirm.*suggest change/ }).click();
    await expect(page.getByRole("alert")).toBeVisible();
    await open(true);
    await expect(page.getByLabel("Ask for an outline change")).toHaveValue(
      request,
    );
    await page.getByRole("button", { name: "Resume proposal" }).click();
    await expect(page.getByText("Retitled", { exact: true })).toBeVisible();
    expect(generations).toBe(1);
    draft = { ...draft, revision: 2 };
    await page.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("out of date");
    await page.getByRole("button", { name: "Reload saved outline" }).click();
    await expect(
      page.getByRole("button", { name: "Apply", exact: true }),
    ).toBeDisabled();
    await page.getByRole("button", { name: "Discard", exact: true }).click();
    await expect(
      page.getByText("Proposal discarded.", { exact: true }),
    ).toBeFocused();
    expect(generations).toBe(1);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}
