import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

test("dark contrast: sign-in welcome", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.goto("/sign-in");
  await expect(page.getByRole("main")).toBeVisible();
  expect(
    (await new AxeBuilder({ page }).withRules(["color-contrast"]).analyze())
      .violations,
  ).toEqual([]);
});

test("dark contrast: public course preview", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.route("**/v1/public/shares/example", (route) =>
    route.fulfill({
      json: {
        course_id: "course",
        title: "Astronomy",
        description: "A course about the sky",
        version: 1,
        lesson_count: 2,
        generated_lesson_count: 1,
        creator: { username: "sam", display_name: "Sam" },
        outline: [
          {
            title: "Stars",
            lessons: [{ title: "Night sky", generated: true }],
          },
        ],
        sample_slides: [],
        enrollment_count: 3,
      },
    }),
  );
  await page.goto("/c/example");
  await expect(page.getByRole("heading", { name: "Astronomy" })).toBeVisible();
  await page.getByRole("button", { name: "See full outline" }).click();
  expect(
    (await new AxeBuilder({ page }).withRules(["color-contrast"]).analyze())
      .violations,
  ).toEqual([]);
});

test("dark contrast: SSO confirmation and invalid link", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.goto(`/sso-bridge?state=${"a".repeat(32)}&strategy=oauth_google`);
  await expect(
    page.getByRole("heading", { name: "Sign in to the Duolify app" }),
  ).toBeVisible();
  expect(
    (await new AxeBuilder({ page }).withRules(["color-contrast"]).analyze())
      .violations,
  ).toEqual([]);
  await page.goto("/sso-bridge");
  await expect(
    page.getByRole("heading", { name: "Sign-in did not finish" }),
  ).toBeVisible();
  expect(
    (await new AxeBuilder({ page }).withRules(["color-contrast"]).analyze())
      .violations,
  ).toEqual([]);
});

for (const theme of ["light", "dark"] as const) {
  test(`checkboxes are circular, keyboard operable and have a large label in ${theme}`, async ({
    page,
  }) => {
    await page.emulateMedia({ colorScheme: theme });
    await page.goto("/sign-in");
    await page.evaluate(() => {
      document.body.insertAdjacentHTML(
        "beforeend",
        '<label id="control-label"><input type="checkbox"> Example</label>',
      );
    });
    const checkbox = page.getByRole("checkbox", { name: "Example" });
    const label = page.locator("#control-label");
    expect((await label.boundingBox())!.height).toBeGreaterThanOrEqual(44);
    expect((await label.boundingBox())!.width).toBeGreaterThanOrEqual(44);
    await expect(checkbox).toHaveCSS("border-radius", "50%");
    const unchecked = await checkbox.evaluate((element) => {
      const s = getComputedStyle(element);
      return [s.borderColor, s.backgroundColor];
    });
    const luminance = (color: string) =>
      color
        .match(/\d+/g)!
        .slice(0, 3)
        .map(Number)
        .map((v) => v / 255)
        .map((v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4))
        .reduce(
          (sum, value, i) => sum + value * [0.2126, 0.7152, 0.0722][i]!,
          0,
        );
    const levels = unchecked.map(luminance).sort((a, b) => b - a);
    expect((levels[0]! + 0.05) / (levels[1]! + 0.05)).toBeGreaterThanOrEqual(3);
    await checkbox.focus();
    await expect(checkbox).toHaveCSS("outline-style", "solid");
    const focusColor = await checkbox.evaluate(
      (element) => getComputedStyle(element).outlineColor,
    );
    const focusLevels = [luminance(focusColor), luminance(unchecked[1]!)].sort(
      (a, b) => b - a,
    );
    expect(
      (focusLevels[0]! + 0.05) / (focusLevels[1]! + 0.05),
    ).toBeGreaterThanOrEqual(3);
    await page.keyboard.press("Space");
    await expect(checkbox).toBeChecked();
    const colors = await checkbox.evaluate((element) => {
      const style = getComputedStyle(element);
      const mark = getComputedStyle(element, "::before");
      const probe = document.createElement("span");
      probe.style.color = "var(--primary)";
      element.parentElement!.append(probe);
      const primary = getComputedStyle(probe).color;
      probe.style.color = "var(--on-primary)";
      const onPrimary = getComputedStyle(probe).color;
      probe.remove();
      return {
        background: style.backgroundColor,
        mark: mark.borderColor,
        primary,
        onPrimary,
      };
    });
    expect(colors.background).toBe(colors.primary);
    expect(colors.mark).toBe(colors.onPrimary);
    await label.click();
    await expect(checkbox).not.toBeChecked();
    await checkbox.evaluate((element: HTMLInputElement) => {
      element.disabled = true;
    });
    await expect(checkbox).toHaveCSS("border-style", "dashed");
    await expect(checkbox).toHaveCSS("opacity", "1");
  });
}

test("select reserves space at the inline end in both directions", async ({
  page,
}) => {
  await page.goto("/sign-in");
  await page.evaluate(() =>
    document.body.insertAdjacentHTML(
      "beforeend",
      '<select aria-label="Example select"><option>Example</option></select>',
    ),
  );
  const select = page.getByLabel("Example select");
  for (const direction of ["ltr", "rtl"]) {
    await select.evaluate(
      (element, dir) => element.setAttribute("dir", dir),
      direction,
    );
    const padding = await select.evaluate((element) => {
      const style = getComputedStyle(element);
      return [
        parseFloat(style.paddingInlineStart),
        parseFloat(style.paddingInlineEnd),
      ];
    });
    expect(padding[1]).toBeGreaterThan(padding[0]);
    expect(padding[1]).toBeGreaterThanOrEqual(32);
  }
});
