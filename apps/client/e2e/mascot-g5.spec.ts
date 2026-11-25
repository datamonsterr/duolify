// Mascot placements (conversational-flows-and-mascot group 5). Mocked: no API, authored harness pages.
import { copyFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { expect, test } from "@playwright/test";

const OUT =
  "/home/dat/.cache/tmp/claude-1000/-home-dat-dev-duolify/3c11ae02-db6a-44d9-9754-88e15e8d834c/scratchpad/mascot-component";
const VIEWPORTS = [
  { name: "390x844", width: 390, height: 844 },
  { name: "1280x800", width: 1280, height: 800 },
] as const;

for (const vp of VIEWPORTS) {
  test.describe(vp.name, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });
    const save = async (
      page: import("@playwright/test").Page,
      info: import("@playwright/test").TestInfo,
      name: string,
    ) => {
      const file = info.outputPath(`${vp.name}-${name}.png`);
      await page.screenshot({ path: file, fullPage: true });
      try {
        mkdirSync(OUT, { recursive: true });
        copyFileSync(file, join(OUT, `g5-${vp.name}-${name}.png`));
      } catch {
        /* owner folder is optional in CI */
      }
    };

    test("eight states render and stay out of the pointer path", async ({
      page,
    }, info) => {
      await page.goto("/mascot.html?size=96");
      await expect(page.locator("figure")).toHaveCount(8);
      const pe = await page
        .locator("figure svg")
        .first()
        .evaluate((n) => getComputedStyle(n).pointerEvents);
      expect(pe).toBe("none");
      await save(page, info, "states");
    });

    test("guided chat shows the avatar and listens while typing", async ({
      page,
    }, info) => {
      await page.goto("/guided-chat.html");
      const avatar = page.locator(".guided-chat-header svg").first();
      await expect(avatar).toHaveAttribute("data-state", "idle");
      await page
        .getByRole("textbox", { name: "Your answer", exact: true })
        .fill("hello");
      await expect(avatar).toHaveAttribute("data-state", "listening");
      await save(page, info, "guided-chat");
    });

    test("framed player companion is 48 px, outside the slide, and hides in immersive", async ({
      page,
    }, info) => {
      await page.goto("/mascot.html?scene=player");
      const box = await page.locator(".m-companion").boundingBox();
      const slide = await page.getByTestId("slide").boundingBox();
      expect(box?.width).toBe(48);
      expect(box!.y + box!.height).toBeLessThanOrEqual(slide!.y);
      await page.getByRole("button", { name: "Buo is celebrating" }).click();
      await expect(page.locator(".m-companion svg")).toHaveAttribute(
        "data-state",
        "celebrate",
      );
      await save(page, info, "player-framed");
      await page.goto("/mascot.html?scene=player&immersive=1");
      await expect(page.locator(".m-companion")).toHaveCount(0);
      await page.getByRole("button", { name: "Buo is talking" }).click();
      await expect(page.locator(".m-companion svg")).toHaveAttribute(
        "data-state",
        "talking",
      );
      await save(page, info, "player-immersive-speaking");
    });

    test("empty, error and offline states carry text and a name", async ({
      page,
    }, info) => {
      await page.goto("/mascot.html?scene=empty");
      await expect(
        page.getByRole("img", { name: "Buo is unsure" }),
      ).toBeVisible();
      await expect(
        page.getByRole("img", { name: "Buo is asleep" }),
      ).toBeVisible();
      await expect(page.getByText("You're offline")).toBeVisible();
      await save(page, info, "empty-error-offline");
    });
  });
}

test("reduced motion renders static poses; RTL keeps art unflipped", async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/mascot.html?dir=rtl&only=celebrate");
  const svg = page.locator("figure svg").first();
  await expect(svg).toHaveAttribute("data-motion", "off");
  const running = await svg.evaluate(
    (n) =>
      n
        .getAnimations({ subtree: true })
        .filter((a) => a.playState === "running").length,
  );
  expect(running).toBe(0);
  const t = await svg.evaluate((n) => getComputedStyle(n).transform);
  expect(t).toBe("none");
});

test("animations pause when the page is hidden", async ({ page }) => {
  await page.goto("/mascot.html?only=idle");
  await expect(page.locator("figure svg").first()).toHaveAttribute(
    "data-paused",
    "false",
  );
  await page.evaluate(() => {
    Object.defineProperty(document, "visibilityState", {
      value: "hidden",
      configurable: true,
    });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await expect(page.locator("figure svg").first()).toHaveAttribute(
    "data-paused",
    "true",
  );
});
