import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";
// Persistent cursors require independent learners between acceptance cases.
resetTokens();
test.afterEach(() => resetTokens());

for (const width of [1280, 390]) {
  test(`authorized object lesson at ${width}px reaches test without completion`, async ({
    page,
    request,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/profile");
    const tokens = await page.evaluate(() =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
    );
    const headers = { Authorization: `Bearer ${tokens.access_token}` };
    const seeded = await request.post(`${apiBase}/test/player/fixture`, {
      headers,
    });
    expect(seeded.status()).toBe(201);
    const { lesson_id } = await seeded.json();
    await page.goto(`/lesson/${lesson_id}`);
    await expect(page.getByText("Slide 1 of 3")).toBeVisible();
    const frame = page.locator('iframe[title="Slide 1"]');
    await expect(frame).toHaveAttribute("sandbox", "allow-scripts");
    await expect(
      page
        .frameLocator('iframe[title="Slide 1"]')
        .getByRole("heading", { name: "Look up" }),
    ).toBeVisible();
    expect(await frame.getAttribute("srcdoc")).toContain("default-src 'none'");
    for (const directive of [
      "base-uri 'none'",
      "form-action 'none'",
      "navigate-to 'none'",
    ])
      expect(await frame.getAttribute("srcdoc")).toContain(directive);
    const storageDenied = await page
      .frameLocator('iframe[title="Slide 1"]')
      .locator("body")
      .evaluate(() => {
        try {
          void parent.localStorage.length;
          return false;
        } catch {
          return true;
        }
      });
    expect(storageDenied).toBe(true);
    await page.screenshot({
      path: testInfo.outputPath("authorized-player.png"),
      fullPage: true,
    });
    await page.getByRole("button", { name: "Transcript", exact: true }).click();
    await expect(
      page.getByRole(width < 768 ? "dialog" : "region", { name: "Transcript" }),
    ).toBeVisible();
    if (width < 768) await page.keyboard.press("Escape");
    else await page.getByRole("button", { name: "Close Transcript" }).click();
    await expect(
      page.getByRole("button", { name: "Transcript", exact: true }),
    ).toBeFocused();
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await page.getByRole("link", { name: "Take lesson test" }).click();
    await expect(page).toHaveURL(`/lesson/${lesson_id}/test`);
    await expect(page.getByText("Lesson complete!")).toHaveCount(0);
    const manifest = await request.get(
      `${apiBase}/v1/lessons/${lesson_id}/manifest`,
      { headers },
    );
    expect(manifest.status()).toBe(200);
    expect(JSON.stringify(await manifest.json())).not.toMatch(
      /answer_key|correct_answer|completed/,
    );
    expect(
      (
        await request.get(`${apiBase}/v1/lessons/${lesson_id}/manifest`)
      ).status(),
    ).toBe(401);
  });
}

test("unavailable and offline lessons have retry and exit recovery", async ({
  page,
}) => {
  await page.route("**/v1/lessons/*/manifest", (route) =>
    route.fulfill({ status: 404, json: { error: "lesson unavailable" } }),
  );
  await page.goto("/lesson/not-enrolled");
  await expect(page.getByRole("alert")).toContainText("Lesson unavailable");
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Exit lesson" })).toBeVisible();
  await page.route("**/v1/lessons/*/manifest", (route) =>
    route.abort("internetdisconnected"),
  );
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(page.getByRole("alert")).toContainText("connection");
});

test("sandbox blocks network and ignores forged host messages", async ({
  page,
  request,
}) => {
  await page.goto("/profile");
  const token = await page.evaluate(
    () =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!)
        .access_token,
  );
  const seeded = await request.post(`${apiBase}/test/player/fixture`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(seeded.status()).toBe(201);
  const { lesson_id } = await seeded.json();
  let thirdPartyRequests = 0;
  await page.route("https://third-party.example.test/**", async (route) => {
    thirdPartyRequests++;
    await route.fulfill({
      status: 200,
      headers: { "Access-Control-Allow-Origin": "*" },
      body: "would succeed without CSP",
    });
  });
  // Simulate an untrusted document with no policy of its own. The host must
  // install policy before its script runs, even if the content source is hostile.
  await page.route(`**/v1/lessons/${lesson_id}/slides/*`, (route) =>
    route.fulfill({
      json: {
        html: `<html><body><h1>Sandbox boundary</h1><script>
    fetch("https://third-party.example.test/probe").then(()=>document.body.dataset.network="allowed",()=>document.body.dataset.network="blocked");
    try {void parent.localStorage.length;document.body.dataset.storage="allowed"}catch {document.body.dataset.storage="blocked"}
    parent.postMessage({type:"ended",completed:true},"*");
    </script></body></html>`,
      },
    }),
  );
  await page.goto(`/lesson/${lesson_id}`);
  await expect(page.getByText("Slide 1 of 3")).toBeVisible();
  await page.evaluate(() =>
    window.postMessage({ type: "ended", completed: true }, "*"),
  );
  const body = page.frameLocator('iframe[title="Slide 1"]').locator("body");
  await expect(body).toHaveAttribute("data-network", "blocked");
  await expect(body).toHaveAttribute("data-storage", "blocked");
  expect(thirdPartyRequests).toBe(0);
  await expect(page.getByText("Slide 1 of 3")).toBeVisible();
  await expect(page.getByText("Lesson complete!")).toHaveCount(0);
});

test("loading, long captions and reduced motion preserve mobile recovery", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  let release!: () => void;
  const waiting = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/v1/lessons/state-fixture/manifest", async (route) => {
    await waiting;
    await route.fulfill({
      json: {
        lesson_id: "state-fixture",
        course_id: "astronomy",
        title:
          "A very long lesson title about Earth and the changing night sky ".repeat(
            5,
          ),
        version: 1,
        asset_origin: "https://assets.example.test",
        slides: [
          {
            slide_id: "long-1",
            duration_ms: 3000,
            html_url: "/v1/lessons/state-fixture/slides/long-1",
            assets: [],
            checkpoints: [],
            narration_status: "disabled",
            narration_text: "A long explanation remains readable. ".repeat(120),
          },
        ],
      },
    });
  });
  await page.route("**/v1/lessons/state-fixture/slides/long-1", (route) =>
    route.fulfill({
      json: { html: "<html><body><h1>Motion explanation</h1></body></html>" },
    }),
  );
  await page.goto("/lesson/state-fixture");
  await expect(page.getByRole("status")).toContainText(
    "Getting your slides ready",
  );
  await expect(page.getByRole("link", { name: "Exit lesson" })).toBeVisible();
  release();
  await expect(page.getByText("Slide 1 of 1")).toBeVisible();
  await expect(
    page.frameLocator('iframe[title="Slide 1"]').locator("html"),
  ).toHaveAttribute("data-motion", "reduce");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await expect(
    page.getByRole("link", { name: "Take lesson test" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Transcript", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Transcript" })).toContainText(
    "A long explanation remains readable.",
  );
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("button", { name: "Transcript", exact: true }),
  ).toBeFocused();
});

test("invalid manifest and missing slide fail closed with a retry", async ({
  page,
}) => {
  await page.route("**/v1/lessons/unsafe/manifest", (route) =>
    route.fulfill({
      json: { lesson_id: "unsafe", answer_key: "private", slides: [] },
    }),
  );
  await page.goto("/lesson/unsafe");
  await expect(page.getByRole("alert")).toContainText("Lesson unavailable");
  await expect(page.locator("iframe")).toHaveCount(0);
  await page.route("**/v1/lessons/unsafe/manifest", (route) =>
    route.fulfill({
      json: {
        lesson_id: "unsafe",
        course_id: "astronomy",
        title: "A missing slide",
        version: 1,
        asset_origin: "https://assets.example.test",
        slides: [
          {
            slide_id: "s1",
            duration_ms: 3000,
            html_url: "/v1/lessons/unsafe/slides/s1",
            assets: [],
            checkpoints: [],
            narration_status: "disabled",
            narration_text: "Captions stay readable.",
          },
        ],
      },
    }),
  );
  await page.route("**/v1/lessons/unsafe/slides/s1", (route) =>
    route.fulfill({ status: 503, json: { error: "lesson unavailable" } }),
  );
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "This slide is unavailable",
  );
  await expect(page.getByText("Captions stay readable.")).toBeVisible();
  await expect(page.locator('iframe[title="Slide 1"]')).toHaveCount(0);
  await page.route("**/v1/lessons/unsafe/slides/s1", (route) =>
    route.fulfill({
      json: { html: "<html><body><h1>Recovered slide</h1></body></html>" },
    }),
  );
  await page.getByRole("button", { name: "Retry slide" }).click();
  await expect(
    page
      .frameLocator('iframe[title="Slide 1"]')
      .getByRole("heading", { name: "Recovered slide" }),
  ).toBeVisible();
});

test("tracer bridge ignores host and preloaded frame key messages", async ({
  page,
}) => {
  await page.goto("/lesson/tracer-default-cast");
  await expect(page.getByRole("slider", { name: "Slide time" })).toHaveValue(
    "0",
  );
  await page.evaluate(() =>
    window.dispatchEvent(
      new MessageEvent("message", {
        origin: "null",
        source: window,
        data: { type: "player_keydown", code: "ArrowRight", key: "ArrowRight" },
      }),
    ),
  );
  await expect(page.getByRole("slider", { name: "Slide time" })).toHaveValue(
    "0",
  );
  await page
    .frameLocator('iframe[title="Preloaded slide"]')
    .locator("body")
    .evaluate(() => {
      parent.postMessage(
        { type: "player_keydown", code: "ArrowRight", key: "ArrowRight" },
        "*",
      );
    });
  await expect(page.getByRole("slider", { name: "Slide time" })).toHaveValue(
    "0",
  );
  await page
    .frameLocator('iframe[title="Slide 1"]')
    .locator("body")
    .evaluate(() => {
      parent.postMessage(
        { type: "player_keydown", code: "ArrowRight", key: "ArrowRight" },
        "*",
      );
    });
  await expect(page.getByRole("slider", { name: "Slide time" })).toHaveValue(
    "500",
  );
});
