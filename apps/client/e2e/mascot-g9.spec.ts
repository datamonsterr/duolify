// "Your guide" picker (conversational-flows-and-mascot group 9). Mocked API; real client build.
import { copyFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { defaultGuideFixture } from "../src/features/characters/fixtures";
import { expect, test, type Page } from "./native-store-fixture";

const OUT =
  "/home/dat/.cache/tmp/claude-1000/-home-dat-dev-duolify/3c11ae02-db6a-44d9-9754-88e15e8d834c/scratchpad/mascot-component";
const CHARACTER_ID = "3b0f5f3e-7a4c-4b8e-9b5f-2f1d0c4e8a11";
const spec = defaultGuideFixture;

type Mascot = Record<string, unknown>;
async function mockApi(
  page: Page,
  state: { mascot: Mascot; characters: unknown[]; posts: Mascot[] },
) {
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
  await page.route("**/v1/me", (r) =>
    r.fulfill({
      json: {
        id: "10000000-0000-4000-8000-000000000001",
        display_name: "Learner",
        locale: "en",
        time_zone: "UTC",
        background: "",
        analytics_opt_out: true,
      },
    }),
  );
  await page.route("**/v1/auth/sessions", (r) => r.fulfill({ json: [] }));
  await page.route("**/v1/characters", (r) =>
    r.fulfill({ json: state.characters }),
  );
  await page.route("**/v1/me/settings", async (r) => {
    const base = {
      learner_id: "10000000-0000-4000-8000-000000000001",
      analytics_opt_out: true,
      appearance: "system",
      motion: "system",
      narration: true,
      narration_speed: 1,
      captions: true,
      text_size: 1,
      ui_sounds: false,
      haptics: "system",
      locale: "en",
      time_zone: "UTC",
      daily_minutes: null,
      daily_xp: null,
      reminder_enabled: null,
      reminder_time: "",
      fullscreen_on_rotate: true,
      autoplay: true,
      auto_send_after_dictation: true,
    };
    if (r.request().method() === "POST") {
      const patch = r.request().postDataJSON().fields.mascot as Mascot;
      state.posts.push(patch);
      state.mascot = { ...state.mascot, ...patch };
      if (patch.palette === null) delete state.mascot.palette;
    }
    await r.fulfill({ json: { ...base, mascot: state.mascot } });
  });
}
const fresh = (): {
  mascot: Mascot;
  characters: unknown[];
  posts: Mascot[];
} => ({
  mascot: { mode: "default", accessories: [], companion_visible: true },
  characters: [],
  posts: [],
});

for (const vp of [
  { name: "390x844", width: 390, height: 844 },
  { name: "1280x800", width: 1280, height: 800 },
]) {
  test(`recolour, accessories, Character, fallback at ${vp.name}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width: vp.width, height: vp.height });
    const state = fresh();
    state.characters = [{ id: CHARACTER_ID, version: 1, spec }];
    await mockApi(page, state);
    await page.goto("/settings");
    await page.getByText("Your guide", { exact: true }).click();
    const preview = page.locator(".your-guide svg").first();

    await page.getByRole("radio", { name: "Blue" }).check();
    await expect.poll(() => state.posts.length).toBe(1);
    expect((state.posts[0].palette as Mascot).body).toBe("#2F8FE8");
    await expect(preview).toHaveCSS("--m-body", "#2F8FE8");

    await page.getByRole("checkbox", { name: "Bow tie" }).check();
    await page.getByRole("checkbox", { name: "Badge" }).check();
    await expect(page.getByRole("checkbox", { name: "Scarf" })).toBeDisabled();
    await expect
      .poll(() => (state.mascot.accessories as string[]).length)
      .toBe(2);

    await page.getByLabel("My Character").selectOption(CHARACTER_ID);
    await expect(page.locator(".your-guide .m-character")).toBeVisible();
    await expect.poll(() => state.mascot.mode).toBe("character");
    const shot = info.outputPath(`g9-${vp.name}-picker.png`);
    await page.screenshot({ path: shot, fullPage: true });

    // The Character is deleted: the server resolves to default and the guide shows built-in Tinko.
    state.characters = [];
    state.mascot = { ...state.mascot, mode: "default" };
    delete state.mascot.character_id;
    await page.reload();
    await page.getByText("Your guide", { exact: true }).click();
    await expect(page.locator(".your-guide .m-character")).toHaveCount(0);
    await expect(preview).toBeVisible();
    await expect(page.getByLabel("My Character")).toHaveCount(0);
    await page.screenshot({
      path: info.outputPath(`g9-${vp.name}-fallback.png`),
      fullPage: true,
    });
    try {
      mkdirSync(OUT, { recursive: true });
      copyFileSync(shot, join(OUT, `g9-${vp.name}-picker.png`));
      copyFileSync(
        info.outputPath(`g9-${vp.name}-fallback.png`),
        join(OUT, `g9-${vp.name}-fallback.png`),
      );
    } catch {
      /* optional */
    }
  });
}

test("a failed save reverts and shows an alert", async ({ page }) => {
  const state = fresh();
  await mockApi(page, state);
  await page.route("**/v1/me/settings", (r) =>
    r.request().method() === "POST"
      ? r.fulfill({ status: 503, json: { error: "x" } })
      : r.fallback(),
  );
  await page.goto("/settings");
  await page.getByText("Your guide", { exact: true }).click();
  await page.getByRole("radio", { name: "Rose" }).check();
  await expect(
    page.getByRole("alert").filter({ hasText: "Couldn't save" }),
  ).toBeVisible();
  await expect(page.getByRole("radio", { name: "Green" })).toBeChecked();
});
