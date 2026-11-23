import { expect, test, type Page } from "./native-store-fixture";

// Selector creator against a mocked API: create, edit, kind and species switch resets.
const shots = process.env.CHARACTER_V2_SHOTS ?? "";
const sizes = [
  { name: "phone", width: 390, height: 844 },
  { name: "desktop", width: 1280, height: 800 },
] as const;

async function mock(page: Page) {
  const created: unknown[] = [];
  const patched: unknown[] = [];
  const v1 = (name: string) => ({
    schema_version: 1,
    rig_version: "original-v1",
    info: { name, bio: "", backstory: "" },
    personality: {
      traits: ["kind"],
      speaking_style: "Clear",
      persona_prompt: "Help",
    },
    appearance: {
      body_shape: "round",
      palette: { body: "#FFD23F", accent: "#EE4266", outline: "#1A1A24" },
      eyes: "round",
      mouth: "smile",
      accessories: [],
    },
    expressions: ["idle"],
    voice_key: "voice-provisional-guide",
  });
  const records: Record<string, unknown>[] = [];
  await page.addInitScript(() =>
    sessionStorage.setItem(
      "duolify.native-store-fixture",
      JSON.stringify({
        access_token: "a",
        refresh_token: "r",
        expires_in: 900,
      }),
    ),
  );
  await page.route("**/v1/**", (route) => route.fulfill({ json: [] }));
  await page.route("**/v1/me", (route) =>
    route.fulfill({
      json: {
        id: "10000000-0000-4000-8000-000000000001",
        display_name: "Learner",
        locale: "en",
        time_zone: "UTC",
        background: "",
        analytics_opt_out: true,
        welcome_state: "completed",
        tour_state: "completed",
      },
    }),
  );
  await page.route(/\/v1\/characters(\/[^/]+)?$/, async (route) => {
    const req = route.request();
    if (req.method() === "GET") return route.fulfill({ json: records });
    const body = req.postDataJSON() as { spec: { info: { name: string } } };
    if (req.method() === "POST") {
      created.push(body.spec);
      const rec = {
        id: "c1",
        version: 1,
        spec: v1(body.spec.info.name),
        document: body.spec,
      };
      records.push(rec);
      return route.fulfill({ status: 201, json: rec });
    }
    patched.push(body);
    const rec = {
      id: "c1",
      version: 2,
      spec: v1(body.spec.info.name),
      document: body.spec,
    };
    records[0] = rec;
    return route.fulfill({ json: rec });
  });
  return { created, patched };
}

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
