import { expect, test, type Page } from "./native-store-fixture";
import { mockCharacters } from "./characters-v2-mock";

// 10.1: a human, an animal, a robot and a fantasy creature built from selectors only.
// No Credits are quoted or spent, and both previews follow every change.
const shots = process.env.CHARACTER_V2_SHOTS ?? "";
const sizes = shots
  ? ([
      { name: "390x844", width: 390, height: 844 },
      { name: "1280x800", width: 1280, height: 800 },
    ] as const)
  : ([{ name: "390x844", width: 390, height: 844 }] as const);
const schemes = shots ? (["light", "dark"] as const) : (["light"] as const);

type Build = {
  kind: string;
  radio: string;
  species?: string;
  look: [group: string, option: string][];
  outfit: [group: string, option: string][];
  name: string;
  expect: Record<string, string>;
};
const builds: Build[] = [
  {
    kind: "human",
    radio: "Human",
    look: [
      ["Hair style", "Bun"],
      ["Eyes", "Wide"],
    ],
    outfit: [["Top", "Hoodie"]],
    name: "Mira",
    expect: { kind: "human" },
  },
  {
    kind: "animal",
    radio: "Animal",
    species: "Cat",
    look: [["Tail", "Fluffy"]],
    outfit: [["Pattern", "Stripes"]],
    name: "Biscuit",
    expect: { kind: "animal", species: "cat" },
  },
  {
    kind: "robot",
    radio: "Robot",
    look: [
      ["Antenna", "Double"],
      ["Moves by", "Wheels"],
    ],
    outfit: [["Pattern", "Dots"]],
    name: "Bolt",
    expect: { kind: "robot" },
  },
  {
    kind: "fantasy",
    radio: "Fantasy",
    species: "Dragon",
    look: [["Wings", "Feathered"]],
    outfit: [["Pattern", "Checks"]],
    name: "Ember",
    expect: { kind: "fantasy", species: "dragon" },
  },
];

const svg = (page: Page, view: "avatar" | "body") =>
  page.getByTestId(`preview-${view}`).locator("svg").innerHTML();
const choose = (page: Page, group: string, option: string) =>
  page
    .getByRole("group", { name: group, exact: true })
    .getByRole("radio", { name: option, exact: true })
    .check({ force: true });

for (const size of sizes)
  for (const scheme of schemes)
    for (const build of builds) {
      test(`build a ${build.kind} from selectors ${size.name} ${scheme}`, async ({
        page,
      }) => {
        await page.setViewportSize({ width: size.width, height: size.height });
        await page.emulateMedia({ colorScheme: scheme });
        const api = await mockCharacters(page);
        const shot = async (n: string) => {
          if (shots)
            await page.screenshot({
              path: `${shots}/selector-${build.kind}-${size.name}-${scheme}-${n}.png`,
            });
        };
        await page.goto("/settings/characters");
        await page.getByRole("button", { name: "New", exact: true }).click();
        await choose(page, "Kind", build.radio);
        if (build.species) await choose(page, "Species", build.species);
        await shot("start");

        // Both previews update live on each Look change.
        await page.getByRole("tab", { name: "Look" }).click();
        for (const [group, option] of build.look) {
          const [avatar, body] = [
            await svg(page, "avatar"),
            await svg(page, "body"),
          ];
          await choose(page, group, option);
          await expect
            .poll(
              async () =>
                (await svg(page, "avatar")) + (await svg(page, "body")),
            )
            .not.toBe(avatar + body);
        }
        const before = await svg(page, "body");
        await page
          .getByRole("group", { name: /^(Main colour|Hair colour)$/ })
          .getByRole("radio")
          .nth(2)
          .check({ force: true });
        await expect.poll(() => svg(page, "body")).not.toBe(before);
        await page.getByRole("tab", { name: "Outfit" }).click();
        for (const [group, option] of build.outfit)
          await choose(page, group, option);
        await page.getByRole("tab", { name: "About" }).click();
        await page.getByLabel("Name", { exact: true }).fill(build.name);
        await choose(page, "Pronouns", "They/them");
        await shot("mid");

        await page.getByRole("button", { name: "Create", exact: true }).click();
        await expect.poll(() => api.created.length).toBe(1);
        await expect(
          page.getByRole("button", { name: /Customize/ }),
        ).toBeVisible();
        await shot("done");
        const doc = api.created[0] as {
          info: { name: string };
          identity: { pronouns: string };
          body: Record<string, string>;
        };
        expect(doc.info.name).toBe(build.name);
        expect(doc.identity.pronouns).toBe("they/them");
        expect(doc.body).toMatchObject(build.expect);
        expect(api.spends).toEqual([]);
      });
    }

test("keyboard and screen reader complete the flow", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const api = await mockCharacters(page);
  await page.goto("/settings/characters");
  await page.getByRole("button", { name: "New", exact: true }).click();
  // Every previews is a named image; every radio group is named.
  for (const img of await page.getByRole("img").all())
    if (await img.getAttribute("aria-label"))
      expect(await img.getAttribute("aria-label")).not.toBe("");
  const robot = page.getByRole("radio", { name: "Robot", exact: true });
  await page.getByRole("radio", { name: "Human", exact: true }).focus();
  await page.keyboard.press("ArrowRight"); // Animal
  await page.keyboard.press("ArrowRight"); // Robot
  await expect(robot).toBeChecked();
  const label = () =>
    page
      .getByTestId("preview-avatar")
      .locator("div[role=img]")
      .getAttribute("aria-label");
  expect(await label()).toMatch(/robot/i);
  // Tabs by arrow keys, About form by keyboard only, then Create with Enter.
  await page.getByRole("tab", { name: "Kind" }).focus();
  for (let i = 0; i < 3; i++) await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("tab", { name: "About" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await page.getByLabel("Name", { exact: true }).fill("Keys");
  await page.getByRole("button", { name: "Create", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect.poll(() => api.created.length).toBe(1);
  expect(api.spends).toEqual([]);
});
