import { test, expect } from "./accounts-fixture";

const neutral = "That username isn't available. Try another.";

for (const mobile of [false, true]) {
  test(`claim a unique username and reject reserved or case-confusable names ${mobile ? "mobile" : "desktop"}`, async ({
    page,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });
    let mine: string | null = null;
    let outage = false;
    const attempts: string[] = [];
    const taken = new Set(["maya"]);
    await page.route("**/v1/features", (r) =>
      r.fulfill({ json: { lesson_test_tracer_enabled: true } }),
    );
    await page.route("**/v1/social/identity", (r) =>
      r.fulfill({
        json: {
          username: mine && {
            username: mine,
            display_name: "Learner",
            avatar: { initial: mine[0].toUpperCase() },
          },
          rules: { min: 3, max: 20 },
        },
      }),
    );
    await page.route("**/v1/social/username", (r) => {
      if (outage)
        return r.fulfill({ status: 503, json: { error: "Try again." } });
      const name: string = r.request().postDataJSON().username;
      attempts.push(name);
      const canonical = name.normalize("NFKC").toLowerCase();
      if (["support", "admin"].includes(canonical))
        return r.fulfill({
          status: 422,
          json: { error: neutral, code: "username_reserved" },
        });
      if (taken.has(canonical))
        return r.fulfill({
          status: 409,
          json: { error: neutral, code: "username_taken" },
        });
      mine = name;
      taken.add(canonical);
      return r.fulfill({
        json: {
          username: {
            username: name,
            display_name: "Learner",
            avatar: { initial: name[0].toUpperCase() },
          },
          rules: { min: 3, max: 20 },
        },
      });
    });

    await page.goto("/friends");
    await expect(
      page.getByRole("heading", { name: "Choose your username" }),
    ).toBeVisible();
    const input = page.getByRole("textbox", { name: "Username" });
    const claim = page.getByRole("button", { name: "Claim username" });

    // Invalid shapes never reach the API.
    await input.fill("ab");
    await claim.click();
    await expect(page.getByRole("alert")).toContainText("3–20 characters");
    await expect(input).toHaveAttribute("aria-invalid", "true");
    await input.fill("bad__name");
    await claim.click();
    await expect(page.getByRole("alert")).toContainText("3–20 characters");
    expect(attempts).toEqual([]);

    // Reserved and case-confusable duplicates get the same neutral copy.
    await input.fill("Support");
    await input.press("Enter");
    await expect(page.getByRole("alert")).toHaveText(neutral);
    await expect(input).toBeFocused();
    await input.fill("mAYA");
    await input.press("Enter");
    await expect(page.getByRole("alert")).toHaveText(neutral);
    await expect(input).toHaveValue("mAYA");
    expect(attempts).toEqual(["Support", "mAYA"]);

    // Offline/server error offers recovery without losing the draft.
    outage = true;
    await input.fill("Maya_Learns");
    await claim.click();
    await expect(page.getByRole("alert")).toContainText("Try again");
    outage = false;
    await page.getByRole("button", { name: "Retry" }).click();

    // Success keeps display case and shows only the minimal identity card.
    const card = page.getByRole("region", { name: "Your public identity" });
    await expect(card).toContainText("@Maya_Learns");
    await expect(card).toContainText("Learner");
    await expect(card).not.toContainText("@@");
    await expect(
      page.getByRole("heading", { name: "Choose your username" }),
    ).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);

    await page.goto("/settings");
    await expect(page.getByText("@Maya_Learns")).toBeVisible();
  });
}
