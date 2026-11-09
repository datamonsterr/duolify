import { test, expect } from "./accounts-fixture";
import tokens from "../../../docs/design/tokens.json" with { type: "json" };
import { courses } from "../src/mocks/handlers";

for (const width of [1280, 390]) {
  for (const mode of ["light", "dark"] as const) {
    test(`Courses uses semantic roles and keyboard navigation at ${width}px in ${mode}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ colorScheme: mode });
      await page.route("http://localhost:8080/courses", (route) =>
        route.fulfill({ json: courses }),
      );
      await page.route("**/courses/astronomy", (route) =>
        route.fulfill({ json: courses[0] }),
      );
      await page.goto("/courses");
      const card = page.getByRole("article", { name: "Everyday astronomy" });
      await expect(card).toBeVisible();
      const expected = Object.fromEntries(
        Object.entries(tokens.color[mode]).map(([name, token]) => [
          name,
          token.$value.hex,
        ]),
      );
      const actual = await page.evaluate(
        (names) =>
          Object.fromEntries(
            names.map((name) => [
              name,
              getComputedStyle(document.documentElement)
                .getPropertyValue(`--${name}`)
                .trim()
                .replace(/^#([\da-f])([\da-f])([\da-f])$/i, "#$1$1$2$2$3$3"),
            ]),
          ),
        Object.keys(expected),
      );
      expect(actual).toEqual(expected);
      for (const [selector, fg, bg] of [
        [".badge-info", "info", "info-bg"],
        [".badge-credit", "credit", "credit-bg"],
        [".badge-streak", "streak", "streak-bg"],
      ]) {
        const colors = await page
          .locator(selector)
          .first()
          .evaluate((el) => {
            const style = getComputedStyle(el);
            const rgb = (value: string) =>
              `#${value
                .match(/\d+/g)!
                .slice(0, 3)
                .map((v) => Number(v).toString(16).padStart(2, "0"))
                .join("")}`;
            return [rgb(style.color), rgb(style.backgroundColor)];
          });
        expect(colors).toEqual([expected[fg], expected[bg]]);
      }
      const primary = page.getByRole("button", { name: "Refresh courses" });
      await expect(primary).toHaveCSS(
        "background-color",
        mode === "light" ? "rgb(31, 122, 13)" : "rgb(108, 212, 58)",
      );
      await primary.hover();
      await expect(primary).toHaveCSS(
        "background-color",
        mode === "light" ? "rgb(26, 107, 10)" : "rgb(134, 224, 90)",
      );
      await primary.focus();
      await expect(primary).toHaveCSS("outline-width", "3px");
      await page.keyboard.down("Space");
      await expect(primary).toHaveCSS("transform", "matrix(1, 0, 0, 1, 0, 3)");
      await page.keyboard.up("Space");
      const title = card.getByRole("link", { name: "Everyday astronomy" });
      const size = await title.boundingBox();
      expect(size!.height).toBeGreaterThanOrEqual(44);
      expect(size!.width).toBeGreaterThanOrEqual(44);
      await title.focus();
      await expect(title).toHaveCSS("outline-width", "3px");
      await page.keyboard.press("Enter");
      await expect(page).toHaveURL(/\/course\/astronomy$/);
      await expect(page.getByRole("link", { name: /Start lesson/ })).toHaveCSS(
        "width",
        "72px",
      );
      await expect(page.getByText(/hearts|reward credits/i)).toHaveCount(0);
    });
  }
}

for (const width of [1280, 320]) {
  test(`Courses preserves recovery controls and long content at ${width}px`, async ({
    page,
    context,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({ reducedMotion: "reduce" });
    let state: "loading" | "error" | "empty" | "ready" = "loading";
    let release!: () => void;
    const wait = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route("http://localhost:8080/courses", async (route) => {
      if (state === "loading") await wait;
      if (state === "error") return route.fulfill({ status: 503, json: {} });
      return route.fulfill({ json: state === "empty" ? [] : courses });
    });
    await page.goto("/courses");
    await expect(
      page.getByRole("status").filter({ hasText: "Loading your courses" }),
    ).toBeVisible();
    state = "error";
    release();
    await expect(page.getByRole("alert")).toContainText(
      "Could not load courses",
    );
    state = "empty";
    await page.getByRole("button", { name: "Retry" }).click();
    await expect(
      page.getByRole("heading", {
        name: "What would you like to keep learning?",
      }),
    ).toBeVisible();
    state = "ready";
    await page.getByRole("button", { name: "Refresh courses" }).click();
    await expect(page.getByRole("article")).toHaveCount(2);
    await page
      .getByRole("searchbox", { name: "Search courses" })
      .fill("private learner query");
    await expect(page.getByText("No courses found")).toBeVisible();
    await page.getByRole("button", { name: "Clear search" }).click();
    await expect(page.getByRole("searchbox")).toBeFocused();
    await context.setOffline(true);
    await expect(
      page.getByRole("status").filter({ hasText: "Offline" }),
    ).toContainText("as of");
    await expect(page.getByRole("article")).toHaveCount(2);
    await context.setOffline(false);
    await page.route("http://localhost:8080/courses", (route) =>
      route.fulfill({
        json: [
          {
            ...courses[0],
            title:
              "A very long course title about astronomy 日本語 العربية Tiếng Việt and the night sky ".repeat(
                5,
              ),
          },
        ],
      }),
    );
    await page.getByRole("button", { name: "Refresh courses" }).click();
    await expect(page.getByRole("article")).toHaveCount(1);
    await page.evaluate(() => {
      document.documentElement.style.fontSize = "200%";
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    const refresh = page.getByRole("button", { name: "Refresh courses" });
    await refresh.focus();
    await page.keyboard.down("Space");
    await expect(refresh).toHaveCSS("transform", "none");
    await page.keyboard.up("Space");
  });
}

test("Nunito weights load from bundled WOFF2 without a font service", async ({
  page,
}) => {
  const external: string[] = [];
  page.on("request", (request) => {
    if (/fonts\.(googleapis|gstatic)\.com|fontsource\.org/.test(request.url()))
      external.push(request.url());
  });
  await page.route("http://localhost:8080/courses", (route) =>
    route.fulfill({ json: courses }),
  );
  await page.goto("/courses");
  const weights = await page.evaluate(async () => {
    for (const weight of [600, 800, 900])
      await document.fonts.load(`${weight} 16px Nunito`);
    return [...document.fonts]
      .filter((font) => font.family === "Nunito" && font.status === "loaded")
      .map((font) => font.weight);
  });
  expect(weights).toEqual(expect.arrayContaining(["600", "800", "900"]));
  expect(external).toEqual([]);
  const fonts = await page.evaluate(() =>
    performance
      .getEntriesByType("resource")
      .filter((entry) => entry.name.endsWith(".woff2"))
      .map((entry) => new URL(entry.name).origin),
  );
  expect(fonts.length).toBeGreaterThanOrEqual(3);
  expect(fonts.every((origin) => origin === new URL(page.url()).origin)).toBe(
    true,
  );
});

test.describe("mobile touch and preferences", () => {
  test.use({ hasTouch: true });
  test("pending refresh, unavailable cards and appearance preserve search without navigation overlap", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme: "light" });
    let hold = false;
    let release!: () => void;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    let requests = 0;
    await page.route("http://localhost:8080/courses", async (route) => {
      requests += 1;
      if (hold) await pending;
      return route.fulfill({
        json: [
          ...courses,
          {
            ...courses[0],
            id: "removed",
            title: "An unavailable course",
            availability: "unavailable",
          },
        ],
      });
    });
    await page.route("**/courses/plants", (route) =>
      route.fulfill({ json: courses[1] }),
    );
    await page.goto("/courses");
    const unavailable = page.getByRole("article", {
      name: "An unavailable course",
    });
    await expect(unavailable).toContainText("Course unavailable");
    await expect(unavailable.getByRole("link")).toHaveCount(0);
    const nav = await page
      .getByRole("navigation", { name: "Main" })
      .boundingBox();
    const main = await page.getByRole("main").boundingBox();
    expect(nav!.y).toBeGreaterThanOrEqual(main!.y + main!.height);
    hold = true;
    await page.getByRole("button", { name: "Refresh courses" }).tap();
    const preparing = page.getByRole("button", { name: "Preparing…" });
    await expect(preparing).toBeDisabled();
    await expect(preparing).toHaveAttribute("aria-busy", "true");
    await expect(preparing).toHaveCSS("background-color", "rgb(221, 241, 255)");
    expect(requests).toBe(2);
    hold = false;
    release();
    await expect(
      page.getByRole("button", { name: "Refresh courses" }),
    ).toBeEnabled();
    await page.getByRole("searchbox").fill("Plant");
    await page.evaluate(() => {
      document.documentElement.dataset.theme = "dark";
      document.documentElement.dataset.motion = "reduce";
    });
    await expect(page.getByRole("searchbox")).toHaveValue("Plant");
    await expect(page.locator(".course-icon")).toHaveCSS(
      "background-color",
      "rgb(38, 56, 63)",
    );
    await page.getByRole("link", { name: "Plant science", exact: true }).tap();
    await expect(page).toHaveURL(/\/course\/plants$/);
  });
});
