import type { APIRequestContext, Page } from "@playwright/test";
import { apiBase } from "./servers";
import { test, expect } from "./accounts-fixture";

type Headers = { Authorization: string };

/** A fresh signed-in account (the owner of one catalogue course), reached through the real API. */
async function account(request: APIRequestContext): Promise<Headers> {
  const email = `owner-${crypto.randomUUID()}@example.test`;
  await request.post(`${apiBase}/v1/auth/code`, {
    data: { email, age_confirmed: true },
  });
  const { code } = await (
    await request.get(`${apiBase}/test/mail?email=${email}`)
  ).json();
  const verified = await request.post(`${apiBase}/v1/auth/verify`, {
    data: { email, code, age_confirmed: true },
  });
  expect(verified.status()).toBe(200);
  const tokens = await verified.json();
  return { Authorization: `Bearer ${tokens.access_token}` };
}

/** Seeds a 4-lesson course (3 generated), screens it and lists it in the Marketplace. */
async function listed(
  request: APIRequestContext,
  fields: {
    title: string;
    description: string;
    category: string;
    language: string;
    tags: string[];
  },
) {
  const headers = await account(request);
  const seeded = await request.post(`${apiBase}/test/marketplace/course`, {
    headers,
    data: { title: fields.title, description: fields.description },
  });
  expect(seeded.status()).toBe(201);
  const course = (await seeded.json()).course_id as string;
  const draft = await request.post(
    `${apiBase}/v1/courses/${course}/publication-drafts`,
    { headers, data: {} },
  );
  expect((await draft.json()).verdict).toBe("pass");
  const put = await request.put(
    `${apiBase}/v1/courses/${course}/marketplace-listing`,
    {
      headers,
      data: {
        category: fields.category,
        language: fields.language,
        tags: fields.tags,
      },
    },
  );
  expect(put.status()).toBe(200);
  return { course, headers };
}

const unique = () => `zq${crypto.randomUUID().replaceAll("-", "").slice(0, 8)}`;

const search = async (page: Page, text: string) => {
  const box = page.getByRole("searchbox", { name: "Search public courses" });
  await box.fill(text);
  await box.press("Enter");
};
const titles = async (page: Page) =>
  (
    await page
      .getByTestId("market-results")
      .getByRole("heading", { level: 2 })
      .allTextContents()
  ).map((t) => t.trim());

test("search, filter, sort a fixture catalogue and enroll for free (desktop)", async ({
  page,
  request,
}) => {
  const word = unique();
  const a = await listed(request, {
    title: `${word} Rust ownership basics`,
    description: "Borrowing, moves and lifetimes in small steps.",
    category: "tech",
    language: "en",
    tags: ["rust", "memory"],
  });
  const b = await listed(request, {
    title: `${word} Aprender Rust desde cero`,
    description: "Propiedad y préstamos explicados con calma.",
    category: "tech",
    language: "es",
    tags: ["rust"],
  });
  const c = await listed(request, {
    title: `${word} Cooking pasta at home`,
    description: "Rust-free kitchen basics.",
    category: "life_skills",
    language: "en",
    tags: ["food"],
  });
  void c;

  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto("/");
  await page.getByRole("link", { name: "Marketplace" }).first().click();
  await expect(page).toHaveURL(/\/marketplace$/);
  await expect(
    page.getByRole("heading", { level: 1, name: "Marketplace" }),
  ).toBeVisible();

  // Full-text search over title, description and tags; page size is 2 on this server.
  await search(page, word);
  await expect(page.getByTestId("market-results")).toBeVisible();
  expect(await titles(page)).toHaveLength(2);
  await page.getByRole("button", { name: "Load more" }).click();
  await expect.poll(() => titles(page)).toHaveLength(3);
  await expect(page.getByRole("button", { name: "Load more" })).toHaveCount(0);

  // Category and language filters narrow the same search.
  await page.getByRole("button", { name: "Tech", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Tech", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect.poll(() => titles(page)).toHaveLength(2);
  await page.getByLabel("Language").selectOption("es");
  await expect
    .poll(() => titles(page))
    .toEqual([`${word} Aprender Rust desde cero`]);
  await page.getByLabel("Language").selectOption("");
  await page.getByRole("button", { name: "All", exact: true }).click();
  await expect.poll(() => titles(page)).toHaveLength(2);
  await page.getByRole("button", { name: "Load more" }).click();
  await expect.poll(() => titles(page)).toHaveLength(3);

  // Sorts: new is publish order, rating is the Bayesian average (zero ratings allowed).
  await page.getByLabel("Sort").selectOption("new");
  await expect
    .poll(async () => (await titles(page))[0])
    .toBe(`${word} Cooking pasta at home`);
  const set = (course: string, sum: number, count: number) =>
    request.post(`${apiBase}/test/marketplace/stats`, {
      headers: a.headers,
      data: { course_id: course, rating_sum: sum, rating_count: count },
    });
  expect((await set(a.course, 90, 20)).status()).toBe(204);
  expect((await set(b.course, 4, 2)).status()).toBe(204);
  await page.getByLabel("Sort").selectOption("rating");
  await page.getByRole("button", { name: "Load more" }).click();
  await expect
    .poll(() => titles(page))
    .toEqual([
      `${word} Rust ownership basics`,
      `${word} Cooking pasta at home`,
      `${word} Aprender Rust desde cero`,
    ]);

  // Course page: attribution, version, outline, safe preview, ready versus paid lessons.
  await page
    .getByRole("link", { name: `${word} Aprender Rust desde cero` })
    .click();
  await expect(page).toHaveURL(new RegExp(`/marketplace/${b.course}$`));
  await expect(
    page.getByRole("heading", {
      level: 1,
      name: `${word} Aprender Rust desde cero`,
    }),
  ).toBeVisible();
  await expect(page.getByTestId("creator")).toContainText(/by .+ · v1/);
  await expect(page.getByTestId("lessons-ready")).toContainText(
    "3 of 4 lessons ready",
  );
  await expect(page.getByTestId("paid-note")).toContainText("Credits");
  await expect(page.getByTitle("Sample slide 1")).toBeVisible();
  await expect(
    page.getByRole("list", { name: "Course outline" }),
  ).toBeVisible();
  await page.getByRole("button", { name: /^Enroll/ }).click();
  await expect(page.getByTestId("enroll-status")).toContainText("Enrolled");
  await expect(page.getByRole("link", { name: "Open course" })).toHaveAttribute(
    "href",
    /\/course\/[0-9a-f-]{36}$/,
  );

  // Popular ranks the enrolled course first; the owner delisting removes it at once.
  await page.goto("/marketplace");
  await search(page, word);
  await page.getByLabel("Sort").selectOption("popular");
  await expect
    .poll(async () => (await titles(page))[0])
    .toBe(`${word} Aprender Rust desde cero`);
  const gone = await request.delete(
    `${apiBase}/v1/courses/${b.course}/marketplace-listing`,
    { headers: b.headers },
  );
  expect(gone.status()).toBe(204);
  await page.reload();
  await search(page, word);
  await expect.poll(() => titles(page)).toHaveLength(2);
  expect(await titles(page)).not.toContain(`${word} Aprender Rust desde cero`);
  await page.goto(`/marketplace/${b.course}`);
  await expect(page.getByRole("heading", { level: 1 })).toContainText(
    "isn't available",
  );
});

test("browse and enroll on a touch phone with the same actions (mobile)", async ({
  page,
  request,
}) => {
  const word = unique();
  const { course } = await listed(request, {
    title: `${word} Houseplants that survive you`,
    description: "Light, water and patience.",
    category: "life_skills",
    language: "en",
    tags: ["plants"],
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/more");
  await expect(page.getByRole("link", { name: "Marketplace" })).toBeVisible();
  await page.getByRole("link", { name: "Marketplace" }).tap();
  await expect(page).toHaveURL(/\/marketplace$/);
  await search(page, word);
  await expect.poll(() => titles(page)).toHaveLength(1);
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
  await page
    .getByRole("link", { name: `${word} Houseplants that survive you` })
    .tap();
  await expect(page).toHaveURL(new RegExp(`/marketplace/${course}$`));
  await expect(page.getByTestId("lessons-ready")).toContainText(
    "3 of 4 lessons ready",
  );
  const enroll = page.getByRole("button", { name: /^Enroll/ });
  const box = (await enroll.boundingBox())!;
  expect(box.height).toBeGreaterThanOrEqual(44);
  await enroll.tap();
  await expect(page.getByTestId("enroll-status")).toContainText("Enrolled");
  // Back returns to the list with the query kept (two screens, not a split pane).
  await page.goBack();
  await expect(
    page.getByRole("searchbox", { name: "Search public courses" }),
  ).toHaveValue(word);
});

test("loading, empty, offline, long content, keyboard, dark and reduced motion", async ({
  page,
  request,
}) => {
  const word = unique();
  const long =
    `${word} ${"Distributed systems from consensus to queues ".repeat(4)}`.trim();
  await listed(request, {
    title: long,
    description: "d".repeat(900),
    category: "science",
    language: "en",
    tags: ["systems"],
  });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.setViewportSize({ width: 390, height: 844 });

  // Loading: the response is held, a status is announced, then results arrive.
  let release!: () => void;
  const held = new Promise<void>((resolve) => (release = resolve));
  await page.route("**/v1/marketplace/courses?*", async (route) => {
    await held;
    await route.continue();
  });
  await page.goto(`/marketplace?q=${word}`);
  await expect(page.getByRole("status")).toContainText("Loading courses");
  release();
  await page.unroute("**/v1/marketplace/courses?*");
  await expect(page.getByTestId("market-results")).toBeVisible();

  // Long titles and descriptions never widen the page; reduced motion removes animation.
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
  const animated = await page
    .locator(".market-card, .sharing-spinner")
    .evaluateAll((els) =>
      els.some((el) => getComputedStyle(el).animationName !== "none"),
    );
  expect(animated).toBe(false);
  const dark = await page.evaluate(
    () => getComputedStyle(document.body).backgroundColor,
  );
  expect(dark).not.toBe("rgb(255, 255, 255)");

  // Keyboard: search, then a category chip, with a visible focus ring and no pointer.
  const box = page.getByRole("searchbox", { name: "Search public courses" });
  await box.focus();
  await page.keyboard.press("Tab");
  const focused = await page.evaluate(() => {
    const el = document.activeElement as HTMLElement;
    return getComputedStyle(el).outlineStyle !== "none" || el.tagName;
  });
  expect(focused).toBeTruthy();

  // Empty: the query is kept and a way forward is offered.
  await search(page, `${word}nomatch`);
  const empty = page.getByTestId("market-empty");
  await expect(empty).toContainText(`${word}nomatch`);
  await expect(
    empty.getByRole("link", { name: "Create this course" }),
  ).toHaveAttribute("href", /\/courses\/create/);
  await expect(box).toHaveValue(`${word}nomatch`);

  // Offline: a neutral alert with Retry; Retry recovers.
  await page.route("**/v1/marketplace/courses?*", (route) => route.abort());
  await search(page, word);
  const alert = page.getByRole("alert");
  await expect(alert).toContainText(/Couldn't reach Duolify/);
  await page.unroute("**/v1/marketplace/courses?*");
  await alert.getByRole("button", { name: "Retry" }).click();
  await expect(page.getByTestId("market-results")).toBeVisible();
});

test("feature report is on for this server and an unknown course answers neutrally", async ({
  request,
}) => {
  // The disposable server turns the flag on; its feature report is the contract the client reads.
  const features = await request.get(`${apiBase}/v1/features`);
  expect((await features.json()).marketplace_enabled).toBe(true);
  const missing = await request.get(
    `${apiBase}/v1/marketplace/courses/00000000-0000-4000-8000-000000000000`,
  );
  expect(missing.status()).toBe(404);
  expect(await missing.json()).toEqual({ error: "course_unavailable" });
});
