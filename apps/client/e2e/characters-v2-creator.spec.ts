import { expect, test } from "./native-store-fixture";
import { mockCharacters as mock } from "./characters-v2-mock";

// Selector creator against a mocked API: create, edit, kind and species switch resets.
const shots = process.env.CHARACTER_V2_SHOTS ?? "";
const sizes = [
  { name: "phone", width: 390, height: 844 },
  { name: "desktop", width: 1280, height: 800 },
] as const;

for (const size of sizes) {
  test(`creator by selectors ${size.name}`, async ({ page }) => {
    await page.setViewportSize({ width: size.width, height: size.height });
    const api = await mock(page);
    await page.goto("/settings/characters");
    await page.getByRole("button", { name: "New", exact: true }).click();
    const shot = async (n: string) => {
      if (shots)
        await page.screenshot({
          path: `${shots}/${size.name}-${n}.png`,
          fullPage: size.name === "phone",
        });
    };
    await shot("kind");
    // Kind then species resets species attributes.
    await page.getByRole("radio", { name: "Animal" }).check({ force: true });
    await page.getByRole("radio", { name: "Cat" }).check({ force: true });
    await page.getByRole("tab", { name: "Look" }).click();
    await page.getByRole("radio", { name: "Fluffy" }).check({ force: true });
    await shot("look");
    await page.getByRole("tab", { name: "Kind" }).click();
    await page.getByRole("radio", { name: "Dog" }).check({ force: true });
    await page.getByRole("tab", { name: "Look" }).click();
    await expect(
      page
        .getByRole("group", { name: "Tail" })
        .getByRole("radio", { name: "Short" }),
    ).toBeChecked();
    await page.getByRole("tab", { name: "Outfit" }).click();
    await page.getByRole("checkbox", { name: "Scarf" }).check({ force: true });
    await shot("outfit");
    await page.getByRole("tab", { name: "About" }).click();
    await page.getByLabel("Name", { exact: true }).fill("Biscuit");
    await shot("about");
    // Keyboard path: tabs move with arrows.
    await page.getByRole("tab", { name: "About" }).focus();
    await page.keyboard.press("ArrowLeft");
    await expect(page.getByRole("tab", { name: "Outfit" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    await page.getByRole("tab", { name: "About" }).click();
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect.poll(() => api.created.length).toBe(1);
    const doc = api.created[0] as {
      info: { name: string };
      body: { kind: string; species: string; tail: string };
      clothes: { accessories: { kind: string }[] };
    };
    expect(doc.info.name).toBe("Biscuit");
    expect(doc.body).toMatchObject({
      kind: "animal",
      species: "dog",
      tail: "short",
    });
    expect(doc.clothes.accessories[0]!.kind).toBe("scarf");
    // Edit.
    await expect(page.getByRole("button", { name: /Customize/ })).toBeVisible();
    await page.getByRole("button", { name: /Customize/ }).click();
    await page.getByRole("tab", { name: "About" }).click();
    await page.getByLabel("Name", { exact: true }).fill("Biscuit II");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect.poll(() => api.patched.length).toBe(1);
    expect(api.patched[0]).toMatchObject({
      expected_version: 1,
      spec: { info: { name: "Biscuit II" } },
    });
  });
}
