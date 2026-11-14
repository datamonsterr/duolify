import type { Locator, Page } from "@playwright/test";
import { test, expect } from "./accounts-fixture";

const course = "c1111111-1111-4111-8111-111111111111";
type Rec = {
  id: string;
  text: string;
  scope: "next_lesson" | "rest_of_course";
  status: "active" | "applied";
  version: number;
  added_at: string;
  applied_lesson_id: string | null;
  delivery: "ready" | "running" | "waits" | "";
};
const rec = (over: Partial<Rec> & Pick<Rec, "id" | "text">): Rec => ({
  scope: "next_lesson",
  status: "active",
  version: 1,
  added_at: "2026-11-07T10:00:00Z",
  applied_lesson_id: null,
  delivery: "",
  ...over,
});
const failure = (status: number, code: string) => ({
  status,
  json: { reason_code: code },
});

async function mockSteering(page: Page, seed: Rec[]) {
  const state = {
    records: seed,
    next: 10,
    patchFailures: [] as { status: number; code: string }[],
    listFailures: 0,
    lists: 0,
    patches: [] as Record<string, unknown>[],
  };
  await page.route("**/v1/features", (r) =>
    r.fulfill({ json: { lesson_test_tracer_enabled: true } }),
  );
  await page.route(`**/v1/courses/${course}`, (r) =>
    r.fulfill({
      json: {
        id: course,
        title: "Navigation",
        units: [
          {
            id: "u1",
            title: "Sky",
            lessons: [
              { id: "l1", title: "Coordinates", goal: "Go", state: "current" },
            ],
          },
        ],
      },
    }),
  );
  await page.route(
    new RegExp(`/v1/courses/${course}/instructions(/[^/?]+)?(\\?.*)?$`),
    async (r) => {
      const req = r.request();
      const id = new URL(req.url()).pathname.split("/")[5];
      const find = () => state.records.find((x) => x.id === id);
      if (req.method() === "GET") {
        state.lists++;
        if (state.listFailures > 0) {
          state.listFailures--;
          return r.abort("failed");
        }
        const active = state.records.filter((x) => x.status === "active");
        return r.fulfill({
          json: {
            instructions: state.records,
            limits: {
              max_characters: 500,
              max_active: 20,
              max_total_characters: 4000,
              adds_per_hour: 10,
            },
            active_count: active.length,
            active_characters: active.reduce(
              (n, x) => n + Array.from(x.text).length,
              0,
            ),
          },
        });
      }
      const body = req.postDataJSON();
      if (req.method() === "POST") {
        const added = rec({
          id: `i${state.next++}`,
          text: body.text,
          scope: body.scope,
          delivery: body.scope === "next_lesson" ? "ready" : "",
        });
        state.records.push(added);
        return r.fulfill({ status: 201, json: added });
      }
      const found = find();
      if (!found) return r.fulfill(failure(404, "not_found"));
      if (req.method() === "PATCH") {
        state.patches.push(body);
        const fail = state.patchFailures.shift();
        if (fail) return r.fulfill(failure(fail.status, fail.code));
        if (body.expected_version !== found.version)
          return r.fulfill(failure(409, "version_conflict"));
        if (found.status !== "active")
          return r.fulfill(failure(409, "not_editable"));
        Object.assign(found, {
          text: body.text ?? found.text,
          scope: body.scope ?? found.scope,
          version: found.version + 1,
        });
        return r.fulfill({ json: found });
      }
      state.records = state.records.filter((x) => x.id !== id);
      return r.fulfill({ json: { id, status: "deleted" } });
    },
  );
  return state;
}

const items = (page: Page) => page.getByRole("listitem");
const item = (page: Page, text: string): Locator =>
  items(page).filter({ hasText: text });

const seeded = () => [
  rec({
    id: "i1",
    text: "Use short sentences",
    scope: "next_lesson",
    delivery: "running",
    added_at: "2026-11-07T10:00:00Z",
  }),
  rec({
    id: "i2",
    text: "More examples in Go",
    scope: "rest_of_course",
    added_at: "2026-11-08T10:00:00Z",
  }),
  rec({
    id: "i3",
    text: "Use a restaurant analogy",
    status: "applied",
    applied_lesson_id: "lesson-6",
    added_at: "2026-11-01T10:00:00Z",
  }),
];

async function open(page: Page) {
  await page.goto(`/course/${course}`);
  await page.getByRole("button", { name: "Steer course" }).click();
}

for (const mobile of [false, true]) {
  const label = mobile ? "mobile" : "desktop";
  test.describe(`E16-02 ${label}`, () => {
    test.use({
      viewport: mobile
        ? { width: 390, height: 844 }
        : { width: 1280, height: 900 },
      hasTouch: mobile,
    });
    test.beforeEach(async ({ page }) => {
      await page.emulateMedia({
        colorScheme: mobile ? "dark" : "light",
        reducedMotion: "reduce",
      });
    });
    const addScreen = async (page: Page) => {
      if (mobile)
        await page.getByRole("button", { name: "Add instruction" }).tap();
    };

    test(`Edit/delete conflicting scopes while generation runs ${label}`, async ({
      page,
    }) => {
      const state = await mockSteering(page, seeded());
      await open(page);
      await expect(
        page.getByText("2 active · 38 / 4,000 characters"),
      ).toBeVisible();
      await expect(
        item(page, "Use short sentences").getByText("Next lesson", {
          exact: true,
        }),
      ).toBeVisible();
      await expect(
        item(page, "Use short sentences").getByText(
          "Used by the lesson being prepared; applied when it is delivered",
        ),
      ).toBeVisible();
      await expect(
        item(page, "More examples in Go").getByText("Rest of course", {
          exact: true,
        }),
      ).toBeVisible();
      await expect(
        item(page, "Use a restaurant analogy").getByText(
          "Applied to lesson lesson-6",
        ),
      ).toBeVisible();
      await expect(
        item(page, "Use a restaurant analogy").getByRole("button", {
          name: "Edit",
        }),
      ).toHaveCount(0);
      await expect(page.getByText(/newest.*wins/i).first()).toBeVisible();
      if (mobile) await expect(page.getByLabel("New instruction")).toBeHidden();

      // Conflicting new rest_of_course instruction.
      await addScreen(page);
      await page.getByLabel("New instruction").fill("Use Rust instead");
      await page
        .getByRole("radio", { name: "Rest of course", exact: true })
        .check();
      await page.getByRole("button", { name: "Save instruction" }).click();
      await expect(item(page, "Use Rust instead")).toBeVisible();
      await expect(
        page.getByText("3 active · 54 / 4,000 characters"),
      ).toBeVisible();
      await expect(page.getByRole("status")).toContainText(/saved/i);

      // Edit text and scope; counter, then a concurrent change gives conflict.
      await item(page, "More examples in Go")
        .getByRole("button", { name: "Edit" })
        .click();
      const edit = page.getByLabel("Edit instruction");
      await expect(edit).toBeFocused();
      await expect(edit).toHaveValue("More examples in Go");
      await edit.fill("More examples in Rust");
      await expect(page.getByText("21 / 500")).toBeVisible();
      state.records.find((x) => x.id === "i2")!.version = 5;
      await page
        .getByRole("group", { name: "Change scope" })
        .getByRole("radio", { name: "Next lesson", exact: true })
        .check();
      await page.getByRole("button", { name: "Save changes" }).click();
      await expect(page.getByRole("alert")).toContainText("version_conflict");
      await expect(edit).toHaveValue("More examples in Rust");
      const listsBefore = state.lists;
      expect(listsBefore).toBeGreaterThan(1);
      state.patchFailures.push({ status: 503, code: "screening_unavailable" });
      await page.getByRole("button", { name: "Retry saving" }).click();
      await expect(page.getByRole("alert")).toContainText(
        "screening_unavailable",
      );
      await expect(edit).toHaveValue("More examples in Rust");
      await page.getByRole("button", { name: "Retry saving" }).click();
      expect(state.patches.at(-1)).toMatchObject({
        expected_version: 5,
        text: "More examples in Rust",
        scope: "next_lesson",
      });
      await expect(page.getByRole("alert")).toHaveCount(0);
      await expect(
        item(page, "More examples in Rust").getByText("Next lesson", {
          exact: true,
        }),
      ).toBeVisible();
      await expect(
        page.getByRole("button", { name: "Edit" }).first(),
      ).toBeVisible();

      // Re-screening rejection keeps the draft and names the reason.
      await item(page, "More examples in Rust")
        .getByRole("button", { name: "Edit" })
        .click();
      state.patchFailures.push({ status: 422, code: "urls_not_allowed" });
      await page.getByLabel("Edit instruction").fill("See https://x.example");
      await page.getByRole("button", { name: "Save changes" }).click();
      await expect(page.getByRole("alert")).toContainText("urls_not_allowed");
      await expect(page.getByLabel("Edit instruction")).toHaveValue(
        "See https://x.example",
      );
      await page.getByRole("button", { name: "Cancel" }).click();
      await expect(item(page, "More examples in Rust")).toBeVisible();

      // Delete with confirm; running instruction keeps its job snapshot.
      await item(page, "Use short sentences")
        .getByRole("button", { name: "Delete" })
        .click();
      await expect(
        page.getByText(/Past lessons never change/).last(),
      ).toBeVisible();
      await page.getByRole("button", { name: "Delete instruction" }).click();
      await expect(item(page, "Use short sentences")).toHaveCount(0);
      await expect(page.getByRole("status")).toContainText(/deleted/i);
      await expect(
        page.getByText("2 active · 37 / 4,000 characters"),
      ).toBeVisible();
      await expect(item(page, "Use a restaurant analogy")).toContainText(
        "Applied to lesson lesson-6",
      );
    });

    test(`E16-02 empty, offline load with Retry loading, and limits ${label}`, async ({
      page,
    }) => {
      const state = await mockSteering(page, []);
      state.listFailures = 1;
      await open(page);
      await expect(page.getByRole("alert")).toContainText(
        "Instruction unavailable",
      );
      state.records = [];
      await page.getByRole("button", { name: "Retry loading" }).click();
      await expect(page.getByText("No instruction yet.")).toBeVisible();
      await expect(
        page.getByText("0 active · 0 / 4,000 characters"),
      ).toBeVisible();
      await expect(page.getByText(/20 active/)).toBeVisible();
    });

    test(`E16-02 long RTL and CJK content wraps ${label}`, async ({ page }) => {
      await mockSteering(page, [
        rec({ id: "i1", text: "語".repeat(500) }),
        rec({
          id: "i2",
          text: "مرحبا ".repeat(100).slice(0, 500),
          scope: "rest_of_course",
        }),
      ]);
      await open(page);
      await expect(
        page.getByText("2 active · 1,000 / 4,000 characters"),
      ).toBeVisible();
      const first = item(page, "語語語").locator("[dir=auto]").first();
      await expect(first).toBeVisible();
      const overflow = await page
        .getByLabel("Course instructions")
        .evaluate((n) => n.scrollWidth > n.clientWidth + 1);
      expect(overflow).toBe(false);
      await item(page, "語語語").getByRole("button", { name: "Edit" }).click();
      await expect(page.getByText("500 / 500")).toBeVisible();
      await page.getByLabel("Edit instruction").fill("語".repeat(501));
      await expect(
        page.getByRole("button", { name: "Save changes" }),
      ).toBeDisabled();
    });
  });
}

test.describe("E16-02 mobile two-screen flow", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });
  test("list screen, add screen with Back, edit screen, focus returns", async ({
    page,
  }) => {
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await mockSteering(page, seeded());
    await open(page);
    const add = page.getByRole("button", { name: "Add instruction" });
    await expect(add).toBeVisible();
    await expect(page.getByLabel("New instruction")).toBeHidden();
    await add.tap();
    await expect(page.getByLabel("New instruction")).toBeVisible();
    await expect(add).toBeHidden();
    await page.getByRole("button", { name: "Back" }).tap();
    await expect(add).toBeFocused();
    await item(page, "More examples in Go")
      .getByRole("button", { name: "Edit" })
      .tap();
    await expect(page.getByLabel("Edit instruction")).toBeVisible();
    await expect(items(page)).toHaveCount(0);
    await page.keyboard.press("Escape");
    await expect(
      item(page, "More examples in Go").getByRole("button", { name: "Edit" }),
    ).toBeFocused();
    await expect(
      page.getByRole("dialog", { name: "Steer your course" }),
    ).toBeVisible();
  });
});

test("E16-02 keyboard-only edit, delete, escape and focus return (desktop)", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockSteering(page, seeded());
  await open(page);
  const row = item(page, "More examples in Go");
  const edit = row.getByRole("button", { name: "Edit" });
  await edit.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByLabel("Edit instruction")).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(edit).toBeFocused();
  await expect(page.getByLabel("Edit instruction")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Steer course" }),
  ).toHaveAttribute("aria-expanded", "true");
  await edit.focus();
  await page.keyboard.press("Enter");
  await page.keyboard.press("Control+a");
  await page.keyboard.type("More examples in Rust");
  await page.keyboard.press("Tab");
  await page.keyboard.press("Tab");
  await expect(
    page.getByRole("button", { name: "Save changes" }),
  ).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("status")).toContainText(/saved/i);
  await expect(
    item(page, "More examples in Rust").getByRole("button", { name: "Edit" }),
  ).toBeFocused();
  const del = item(page, "More examples in Rust").getByRole("button", {
    name: "Delete",
  });
  await del.focus();
  await page.keyboard.press("Enter");
  await page.keyboard.press("Escape");
  await expect(del).toBeFocused();
  await page.keyboard.press("Enter");
  await page.getByRole("button", { name: "Delete instruction" }).focus();
  await page.keyboard.press("Enter");
  await expect(item(page, "More examples in Rust")).toHaveCount(0);
  await expect(page.getByRole("status")).toContainText(/deleted/i);
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("button", { name: "Steer course" }),
  ).toBeFocused();
});

test("E16-02 real API: edits and deletes leave the running job snapshot unchanged", async ({
  page,
  request,
}) => {
  const { apiBase } = await import("./servers");
  await page.route("**/v1/features", (r) =>
    r.fulfill({ json: { lesson_test_tracer_enabled: true } }),
  );
  await page.goto("/profile");
  const tokens = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
  );
  const headers = { Authorization: `Bearer ${tokens.access_token}` };
  const seed = await request.post(`${apiBase}/test/steering/course`, {
    headers,
  });
  expect(seed.status()).toBe(201);
  const { course_id } = await seed.json();
  await page.route(`**/v1/courses/${course_id}**`, async (route) => {
    const original = route.request();
    const target = new URL(original.url());
    target.host = new URL(apiBase).host;
    await route.fulfill({
      response: await request.fetch(target.href, {
        headers,
        method: original.method(),
        data: original.postData() ?? undefined,
      }),
    });
  });
  await page.goto(`/course/${course_id}`);
  await page.getByRole("button", { name: "Steer course" }).click();
  const add = async (text: string, scope: string) => {
    await page.getByLabel("New instruction").fill(text);
    await page.getByRole("radio", { name: scope, exact: true }).first().check();
    await page.getByRole("button", { name: "Save instruction" }).click();
    await expect(item(page, text)).toBeVisible();
  };
  await add("More examples in Go", "Rest of course");
  await add("Use short sentences", "Next lesson");
  const job = await request.post(`${apiBase}/test/steering/running-job`, {
    headers,
    data: { course_id },
  });
  expect(job.status()).toBe(201);
  const { job_id } = await job.json();
  await add("Use Rust instead", "Rest of course");
  await item(page, "More examples in Go")
    .getByRole("button", { name: "Edit" })
    .click();
  await page.getByLabel("Edit instruction").fill("More examples in Rust");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(item(page, "More examples in Rust")).toBeVisible();
  await item(page, "Use short sentences")
    .getByRole("button", { name: "Delete" })
    .click();
  await page.getByRole("button", { name: "Delete instruction" }).click();
  await expect(item(page, "Use short sentences")).toHaveCount(0);
  const snap = await request.get(
    `${apiBase}/test/steering/snapshot?job_id=${job_id}`,
    { headers },
  );
  expect(snap.status()).toBe(200);
  expect((await snap.json()).texts.slice().sort()).toEqual([
    "More examples in Go",
    "Use short sentences",
  ]);
  const done = await request.post(`${apiBase}/test/steering/finish-job`, {
    headers,
    data: { job_id },
  });
  expect(done.status()).toBe(200);
  await page.goto(`/course/${course_id}`);
  await page.getByRole("button", { name: "Steer course" }).click();
  await expect(
    item(page, "More examples in Rust").getByText("Active", { exact: true }),
  ).toBeVisible();
  await expect(
    item(page, "Use Rust instead").getByText("Active", { exact: true }),
  ).toBeVisible();
  await expect(item(page, "Use short sentences")).toHaveCount(0);
});
