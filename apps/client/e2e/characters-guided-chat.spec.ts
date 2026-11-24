import { expect, test, type Page } from "./native-store-fixture";
import { mockCharacters, v1 } from "./characters-v2-mock";

// character.create and character.cast as guided chats: selectors as chat widgets, free of
// Credits, keyboard reachable. Mocked API; ports come from the guided Playwright config.
const shots = process.env.CHARACTER_V2_SHOTS ?? "";
const sizes = shots
  ? ([
      { name: "390x844", width: 390, height: 844 },
      { name: "1280x800", width: 1280, height: 800 },
    ] as const)
  : ([{ name: "390x844", width: 390, height: 844 }] as const);
const schemes = shots ? (["light", "dark"] as const) : (["light"] as const);

const answer = (page: Page, label: string) =>
  page.getByRole("button", { name: label, exact: true }).click();
const pickTile = (page: Page, group: string, option: string) =>
  page
    .getByRole("group", { name: group, exact: true })
    .getByRole("radio", { name: option, exact: true })
    .check({ force: true });

for (const size of sizes)
  for (const scheme of schemes) {
    const suffix = `${size.name}-${scheme}`;
    const shot = async (page: Page, name: string, n: string) => {
      if (shots) {
        await page.waitForTimeout(500);
        await page.screenshot({
          path: `${shots}/chat-${name}-${suffix}-${n}.png`,
        });
      }
    };

    test(`character.create chat ${suffix}`, async ({ page }) => {
      await page.setViewportSize({ width: size.width, height: size.height });
      await page.emulateMedia({ colorScheme: scheme });
      const api = await mockCharacters(page);
      await page.goto("/settings/characters");
      await page.getByRole("button", { name: "Create in chat" }).click();
      await expect(
        page.getByText("What kind of character do you want?").first(),
      ).toBeVisible();
      await expect(page.getByTestId("preview-avatar")).toBeVisible();
      await expect(page.getByTestId("preview-body")).toBeVisible();
      await shot(page, "create", "start");
      await pickTile(page, "Kind", "Animal");
      await pickTile(page, "Species", "Cat");
      const before = await page.getByTestId("preview-body").innerHTML();
      await answer(page, "Looks good");
      await expect(
        page.getByText("Shape how they look.").first(),
      ).toBeVisible();
      await pickTile(page, "Tail", "Fluffy");
      await expect
        .poll(() => page.getByTestId("preview-body").innerHTML())
        .not.toBe(before);
      await answer(page, "Looks good");
      await expect(page.getByText("Pick their outfit.").first()).toBeVisible();
      await page
        .getByRole("checkbox", { name: "Scarf" })
        .check({ force: true });
      await shot(page, "create", "mid");
      await answer(page, "Looks good");
      // Name: inline error with role=alert, then the Send arrow.
      const name = page.getByRole("textbox", { name: "What's their name?" });
      await name.fill("x".repeat(45));
      await page.getByRole("button", { name: "Send" }).click();
      await expect(page.getByRole("alert")).toBeVisible();
      await name.fill("Biscuit");
      await page.getByRole("button", { name: "Send" }).click();
      // A tap on a chip answers at once.
      await page
        .getByRole("radio", { name: "They/them" })
        .click({ force: true });
      await expect(
        page.getByText("Create this character? It's free.").first(),
      ).toBeVisible();
      await shot(page, "create", "confirm");
      await page.getByRole("button", { name: "Create character" }).click();
      await expect.poll(() => api.created.length).toBe(1);
      await expect(
        page.getByText("Character created. Nothing was spent."),
      ).toBeVisible();
      await shot(page, "create", "done");
      const doc = api.created[0] as {
        info: { name: string };
        identity: { pronouns: string };
        body: Record<string, string>;
        clothes: { accessories: { kind: string }[] };
      };
      expect(doc.info.name).toBe("Biscuit");
      expect(doc.identity.pronouns).toBe("they/them");
      expect(doc.body).toMatchObject({
        kind: "animal",
        species: "cat",
        tail: "fluffy",
      });
      expect(doc.clothes.accessories.map((a) => a.kind)).toContain("scarf");
      expect(api.spends).toEqual([]);
      await page.getByRole("button", { name: "Done" }).click();
      await expect(
        page.getByRole("button", { name: "Create in chat" }),
      ).toBeVisible();
    });

    test(`character.cast chat ${suffix}`, async ({ page }) => {
      await page.setViewportSize({ width: size.width, height: size.height });
      await page.emulateMedia({ colorScheme: scheme });
      const api = await mockCharacters(page);
      const saved: unknown[] = [];
      const own = (id: string, name: string) => ({
        id,
        version: 1,
        spec: v1(name),
      });
      await page.route(/\/v1\/characters$/, (r) =>
        r.fulfill({ json: [own("c1", "Mira"), own("c2", "Bolt")] }),
      );
      await page.route(/\/v1\/courses\/[^/]+\/cast$/, (r) => {
        if (r.request().method() === "PUT") {
          saved.push(r.request().postDataJSON());
          return r.fulfill({ json: saved[0] });
        }
        return r.fulfill({ json: { guide: "", supporting: [] } });
      });
      await page.goto("/courses/course1/cast");
      await page.getByRole("button", { name: "Choose in chat" }).click();
      await expect(
        page.getByText("Who should guide this course?").first(),
      ).toBeVisible();
      await shot(page, "cast", "start");
      await page
        .getByRole("radio", { name: "Mira", exact: true })
        .check({ force: true });
      await answer(page, "Choose");
      await expect(
        page.getByText("Add a supporting character?").first(),
      ).toBeVisible();
      await expect(
        page.getByRole("radio", { name: "Mira", exact: true }),
      ).toHaveCount(0);
      await page
        .getByRole("radio", { name: "Bolt", exact: true })
        .check({ force: true });
      await shot(page, "cast", "mid");
      await answer(page, "Choose");
      await page
        .getByRole("radio", { name: "No one", exact: true })
        .check({ force: true });
      await answer(page, "Choose");
      await expect(
        page.getByText("Save this Course Cast? It's free.").first(),
      ).toBeVisible();
      await page.getByRole("button", { name: "Save Cast" }).click();
      await expect(page.getByText(/Course Cast saved/)).toBeVisible();
      await shot(page, "cast", "done");
      expect(saved).toEqual([{ guide: "c1", supporting: ["c2"] }]);
      expect(api.spends).toEqual([]);
    });
  }

test("character.create chat works by keyboard alone", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const api = await mockCharacters(page);
  await page.goto("/settings/characters");
  await page.getByRole("button", { name: "Create in chat" }).focus();
  await page.keyboard.press("Enter");
  const robot = page.getByRole("radio", { name: "Robot", exact: true });
  await page.getByRole("radio", { name: "Human", exact: true }).focus();
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowRight");
  await expect(robot).toBeChecked();
  for (const next of ["Looks good", "Looks good", "Looks good"]) {
    await page.getByRole("button", { name: next, exact: true }).focus();
    await page.keyboard.press("Enter");
    await page.waitForTimeout(150);
  }
  const name = page.getByRole("textbox", { name: "What's their name?" });
  await name.fill("Keys");
  await page.keyboard.press("Enter");
  await page.getByRole("radio", { name: "Not set" }).focus();
  await page.keyboard.press("Space");
  await page.keyboard.press("Enter");
  await page.getByRole("button", { name: "Create character" }).focus();
  await page.keyboard.press("Enter");
  await expect.poll(() => api.created.length).toBe(1);
  expect((api.created[0] as { body: { kind: string } }).body.kind).toBe(
    "robot",
  );
});
