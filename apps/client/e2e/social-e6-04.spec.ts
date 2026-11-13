import type { APIRequestContext, Page } from "@playwright/test";
import { apiBase } from "./servers";
import { test, expect } from "./accounts-fixture";

// Real API: the browser's /v1/social calls are forwarded to the E2E API server.
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

type Headers = { Authorization: string };

async function signIn(request: APIRequestContext): Promise<Headers> {
  const email = `e6-04-${crypto.randomUUID()}@example.test`;
  await request.post(`${apiBase}/v1/auth/code`, {
    data: { email, age_confirmed: true },
  });
  const { code } = await (
    await request.get(`${apiBase}/test/mail?email=${email}`)
  ).json();
  const verified = await request.post(`${apiBase}/v1/auth/verify`, {
    data: { email, code, age_confirmed: true },
  });
  return { Authorization: `Bearer ${(await verified.json()).access_token}` };
}

async function learner(request: APIRequestContext, name: string) {
  const headers = await signIn(request);
  expect(
    (
      await request.put(`${apiBase}/v1/social/username`, {
        headers,
        data: { username: name },
      })
    ).status(),
  ).toBe(200);
  return headers;
}

const call = (
  request: APIRequestContext,
  headers: Headers,
  method: string,
  path: string,
  data?: unknown,
) => request.fetch(`${apiBase}/v1/social${path}`, { method, headers, data });

/** The browser's own learner (shared across tests): its token and username. */
async function me(page: Page, request: APIRequestContext, fallback: string) {
  const token = await page.evaluate(
    () =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture") ?? "{}")
        .access_token as string,
  );
  const headers = { Authorization: `Bearer ${token}` };
  const identity = await (
    await call(request, headers, "GET", "/identity")
  ).json();
  if (!identity.username) {
    await call(request, headers, "PUT", "/username", { username: fallback });
    return { headers, name: fallback };
  }
  return { headers, name: identity.username.username as string };
}

const befriend = async (
  request: APIRequestContext,
  from: Headers,
  to: Headers,
  toName: string,
) => {
  const sent = await (
    await call(request, from, "POST", "/friend-requests", { username: toName })
  ).json();
  expect(
    (
      await call(request, to, "POST", `/friend-requests/${sent.id}/accept`)
    ).status(),
  ).toBe(200);
};

for (const mobile of [false, true]) {
  test(`block a friend, restrict discovery, unblock ${mobile ? "mobile" : "desktop"}`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });
    await page.route("**/v1/features", (r) =>
      r.fulfill({ json: { lesson_test_tracer_enabled: true } }),
    );
    await proxySocial(page, request);
    const p = `t${Math.random().toString(36).slice(2, 6)}`;
    await page.goto("/friends");
    const mine = await me(page, request, `${p}.mina`);
    const sam = await learner(request, `${p}.sam`);
    const kai = await learner(request, `${p}.kai`);
    const lena = await learner(request, `${p}.lena`);
    await befriend(request, sam, mine.headers, mine.name);
    await befriend(request, kai, mine.headers, mine.name);
    expect(
      (
        await call(request, lena, "POST", "/friend-requests", {
          username: mine.name,
        })
      ).status(),
    ).toBe(200);

    // Offline/error: a failed blocked-list load explains and recovers.
    await page.route("**/v1/social/blocks", (route) => route.abort());
    await page.goto("/friends");
    await expect(page.getByRole("alert")).toContainText(
      "Check your connection",
    );
    await page.unroute("**/v1/social/blocks");
    await proxySocial(page, request);
    await page.getByRole("button", { name: "Retry" }).click();
    await expect(page.getByRole("tab", { name: "Blocked · 0" })).toBeVisible();

    const open = async (name: string) => {
      await page
        .getByRole("button", { name: /^Open / })
        .filter({ hasText: `@${name}` })
        .click();
    };
    const friends = page.getByRole("list", { name: "Your friends" });
    await expect(friends).toContainText(`@${p}.sam`);

    // Block from the friend card: keyboard, focus return, then confirm.
    await open(`${p}.sam`);
    const trigger = page.getByRole("button", { name: "Block", exact: true });
    await trigger.focus();
    await trigger.press("Enter");
    const confirm = page.getByRole("dialog", { name: /^Block .*\?$/ });
    await expect(confirm).toContainText("isn't notified");
    await expect(confirm).toContainText("can't find you");
    await page.keyboard.press("Escape");
    await expect(confirm).toBeHidden();
    await expect(trigger).toBeFocused();
    await trigger.press("Enter");
    await confirm.getByRole("button", { name: "Block", exact: true }).click();
    await expect(friends).not.toContainText(`@${p}.sam`);
    await expect(page.getByRole("tab", { name: "Blocked · 1" })).toBeVisible();

    // The blocked learner only lost the friendship; every route to us fails neutrally.
    expect(await (await call(request, sam, "GET", "/friends")).json()).toEqual({
      friends: [],
    });
    expect(await (await call(request, sam, "GET", "/blocks")).json()).toEqual({
      blocks: [],
    });
    const refused = await call(request, sam, "POST", "/friend-requests", {
      username: mine.name,
    });
    const unknown = await call(request, sam, "POST", "/friend-requests", {
      username: `${p}.nobody`,
    });
    expect(refused.status()).toBe(404);
    expect(await refused.json()).toEqual(await unknown.json());
    const found = await (
      await call(request, sam, "GET", `/search?q=${mine.name}`)
    ).json();
    expect(found.results).toEqual([]);
    expect(
      (await call(request, sam, "GET", `/profiles/${mine.name}`)).status(),
    ).toBe(404);

    // Blocked list: unblock restores discovery, not the friendship.
    await page.getByRole("tab", { name: /^Blocked · / }).click();
    const blocked = page.getByRole("list", { name: "Blocked learners" });
    await expect(blocked).toContainText(`@${p}.sam`);
    const unblock = page.getByRole("button", { name: `Unblock ${p}.sam` });
    await unblock.focus();
    await unblock.press("Enter");
    await expect(blocked).toHaveCount(0);
    await expect(page.getByText("You haven't blocked anyone.")).toBeVisible();
    const back = await (
      await call(request, sam, "GET", `/search?q=${mine.name}`)
    ).json();
    expect(back.results.map((r: { username: string }) => r.username)).toContain(
      mine.name,
    );
    await page.getByRole("tab", { name: /^Friends · / }).click();
    await expect(friends).not.toContainText(`@${p}.sam`);

    // "Only remove friend" in the block dialog ends the friendship without blocking.
    await open(`${p}.kai`);
    await page.getByRole("button", { name: "Block", exact: true }).click();
    await page
      .getByRole("dialog", { name: /^Block .*\?$/ })
      .getByRole("button", { name: "Only remove friend" })
      .click();
    await expect(friends).toHaveCount(0);
    await expect(page.getByRole("tab", { name: "Blocked · 0" })).toBeVisible();
    expect(await (await call(request, kai, "GET", "/blocks")).json()).toEqual({
      blocks: [],
    });

    // Block by username cancels a pending request in both directions.
    await page.getByRole("tab", { name: /^Blocked · / }).click();
    await page
      .getByRole("textbox", { name: "Username to block" })
      .fill(`${p}.lena`);
    await page.getByRole("button", { name: "Block someone" }).click();
    await page
      .getByRole("dialog", { name: /^Block .*\?$/ })
      .getByRole("button", { name: "Block", exact: true })
      .click();
    await expect(
      page.getByRole("list", { name: "Blocked learners" }),
    ).toContainText(`@${p}.lena`);
    const lenaSent = await (
      await call(request, lena, "GET", "/friend-requests")
    ).json();
    expect(lenaSent.outgoing).toEqual([]);

    // A name that does not exist fails with neutral copy and blocks nothing.
    await page
      .getByRole("textbox", { name: "Username to block" })
      .fill(`${p}.nobody`);
    await page.getByRole("button", { name: "Block someone" }).click();
    await page
      .getByRole("dialog", { name: /^Block .*\?$/ })
      .getByRole("button", { name: "Block", exact: true })
      .click();
    await expect(page.getByRole("alert")).toContainText(
      "We couldn't block that person.",
    );
    await expect(page.getByRole("alert")).not.toContainText("exist");
  });
}

test("course invite setting saves and applies server-side", async ({
  page,
  request,
}) => {
  await page.route("**/v1/features", (r) =>
    r.fulfill({ json: { lesson_test_tracer_enabled: true } }),
  );
  await proxySocial(page, request);
  await page.goto("/settings");
  const mine = await me(
    page,
    request,
    `t${Math.random().toString(36).slice(2, 6)}.set`,
  );
  await page.goto("/settings");
  const select = page.getByRole("combobox", { name: "Course invites from" });
  await expect(select).toHaveValue("friends");
  await select.selectOption("nobody");
  await expect(page.getByText("Saved.")).toBeVisible();
  const settings = await (
    await call(request, mine.headers, "GET", "/settings")
  ).json();
  expect(settings.course_invites_from).toBe("nobody");
  await page.reload();
  await expect(
    page.getByRole("combobox", { name: "Course invites from" }),
  ).toHaveValue("nobody");
});
