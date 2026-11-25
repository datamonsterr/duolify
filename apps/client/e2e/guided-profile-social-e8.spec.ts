import type { APIRequestContext, Page } from "@playwright/test";
import { apiBase } from "./servers";
import { test, expect, resetTokens } from "./accounts-fixture";

// Profile edit, daily goal + reminder clock, username and add friend as chats (flag `rest`,
// default on). The old forms are covered by the specs that switch the flag off.
// Set APPROVAL_DIR to also write the owner-gate screenshots (start, mid with widget, done).
const dir = process.env.APPROVAL_DIR;
const shot = async (page: Page, flow: string, step: string, tag: string) => {
  if (!dir) return;
  await page.waitForTimeout(1200); // bubble animation and mascot chunk settle
  await page.screenshot({ path: `${dir}/${flow}-${tag}-${step}.png` });
};
const send = (page: Page) =>
  page
    .locator(".gc-widget")
    .getByRole("button", { name: "Send", exact: true })
    .click();

async function proxySocial(page: Page, request: APIRequestContext) {
  await page.route("**/v1/social/**", async (route) => {
    const original = route.request();
    const target = new URL(original.url());
    const headers = original.headers();
    delete headers.origin;
    target.host = new URL(apiBase).host;
    target.protocol = new URL(apiBase).protocol;
    await route.fulfill({
      response: await request.fetch(target.href, {
        method: original.method(),
        headers,
        data: original.postData() ?? undefined,
      }),
    });
  });
}

async function friend(request: APIRequestContext, name: string) {
  const email = `e8-${crypto.randomUUID()}@example.test`;
  await request.post(`${apiBase}/v1/auth/code`, {
    data: { email, age_confirmed: true },
  });
  const { code } = await (
    await request.get(`${apiBase}/test/mail?email=${email}`)
  ).json();
  const verified = await request.post(`${apiBase}/v1/auth/verify`, {
    data: { email, code, age_confirmed: true },
  });
  const token = (await verified.json()).access_token as string;
  const claimed = await request.put(`${apiBase}/v1/social/username`, {
    headers: { Authorization: `Bearer ${token}` },
    data: { username: name },
  });
  expect(claimed.status()).toBe(200);
}

test.afterEach(() => {
  resetTokens();
});

for (const [width, height, scheme] of [
  [390, 844, "light"],
  [390, 844, "dark"],
  [1280, 800, "light"],
  [1280, 800, "dark"],
] as const) {
  const tag = `${width}-${scheme}`;
  test.describe(`${tag}`, () => {
    test.beforeEach(async ({ page }) => {
      await page.setViewportSize({ width, height });
      await page.emulateMedia({ colorScheme: scheme });
    });

    test("daily goal and reminder clock as a chat", async ({ page }) => {
      await page.goto("/settings#goals");
      // The old fields are gone behind the flag; one chat replaces them.
      await expect(page.getByLabel("Daily minutes")).toHaveCount(0);
      await page
        .getByRole("button", { name: "Daily goal", exact: true })
        .click();
      const dialog = page.getByRole("dialog", { name: "Daily goal" });
      await expect(
        dialog.getByText("How many minutes a day?").first(),
      ).toBeVisible();
      await shot(page, "goal", "start", tag);
      await send(page);
      await send(page);
      await page.getByRole("radio", { name: "Yes" }).click();
      const clock = dialog.locator('input[type="time"]');
      await expect(clock).toBeVisible();
      await expect(clock).toHaveAccessibleName(/^What time\?/);
      await clock.fill("08:30");
      await shot(page, "goal", "mid", tag);
      await send(page);
      await dialog.getByRole("button", { name: "Confirm" }).click();
      await expect(dialog.getByText("Daily goal saved.")).toBeVisible();
      await shot(page, "goal", "done", tag);
      await dialog.locator("[data-action-footer]").getByRole("button").click();
      await expect(page.getByText("Reminder at 08:30")).toBeVisible();
      await page.reload();
      await expect(page.getByText("Reminder at 08:30")).toBeVisible();
    });

    test("profile edit as a chat", async ({ page }) => {
      await page.goto("/profile");
      await expect(
        page.getByText("What do you want to learn?").first(),
      ).toBeVisible();
      await expect(page.getByLabel("Goal", { exact: true })).toHaveCount(0);
      await shot(page, "profile", "start", tag);
      await page.getByRole("radio", { name: "For fun" }).click();
      await page
        .getByRole("textbox", { name: "Which topic are you on?" })
        .fill("Italian cooking");
      await send(page);
      await shot(page, "profile", "mid", tag);
      await page.getByRole("button", { name: "Skip" }).click();
      await page.getByRole("radio", { name: "Some" }).click();
      await expect(page.getByText("Save these?").first()).toBeVisible();
      await page.getByRole("button", { name: "Confirm" }).click();
      await expect(page.getByText("Profile saved.")).toBeVisible();
      await shot(page, "profile", "done", tag);
      await page.reload();
      await expect(
        page.getByRole("button", { name: "Change answer: Italian cooking" }),
      ).toBeVisible();
    });

    test("username then add friend as chats", async ({ page, request }) => {
      await page.route("**/v1/features", (r) =>
        r.fulfill({ json: { lesson_test_tracer_enabled: true } }),
      );
      await proxySocial(page, request);
      const p = `g${Math.random().toString(36).slice(2, 6)}`;
      await friend(request, `${p}.anna`);
      await page.goto("/friends");
      const name = page.getByRole("textbox", { name: /username/i });
      await expect(name).toBeVisible();
      await shot(page, "username", "start", tag);
      await name.fill("x");
      await send(page);
      await expect(page.getByRole("alert").first()).toBeVisible();
      await shot(page, "username", "mid", tag);
      await name.fill(`${p}.mina`);
      await send(page);
      await page.getByRole("button", { name: "Save username" }).click();
      await expect(
        page.getByRole("region", { name: "Your public identity" }),
      ).toContainText(`@${p}.mina`);
      await shot(page, "username", "done", tag);

      await page.getByRole("button", { name: "Add", exact: true }).click();
      const dialog = page.getByRole("dialog", { name: "Add a friend" });
      const who = dialog.getByRole("textbox", { name: /username/i });
      await expect(who).toBeFocused();
      await shot(page, "friend", "start", tag);
      await who.fill(`${p}.nobody`);
      await send(page);
      await expect(
        dialog.getByText("We couldn't find that username."),
      ).toBeVisible();
      await shot(page, "friend", "mid", tag);
      await who.fill(`@${p}.Anna`);
      await send(page);
      await dialog.getByRole("button", { name: "Send request" }).click();
      await expect(dialog.getByText("Request sent.")).toBeVisible();
      await shot(page, "friend", "done", tag);
    });
  });
}
