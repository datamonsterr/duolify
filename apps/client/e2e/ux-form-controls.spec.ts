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

test("checkboxes are circular, keyboard operable and have a large label", async ({
  page,
}) => {
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
  await checkbox.focus();
  await expect(checkbox).toHaveCSS("outline-style", "solid");
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
});

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
