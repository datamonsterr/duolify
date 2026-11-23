import { expect, test } from "@playwright/test";

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
  await expect(checkbox).toHaveCSS("border-radius", "50%");
  await checkbox.focus();
  await expect(checkbox).toHaveCSS("outline-style", "solid");
  await page.keyboard.press("Space");
  await expect(checkbox).toBeChecked();
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
  }
});
