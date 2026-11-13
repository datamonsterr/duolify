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
  const email = `e6-07-${crypto.randomUUID()}@example.test`;
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

async function friendshipId(
  request: APIRequestContext,
  headers: Headers,
  username: string,
) {
  const { friends } = await (
    await call(request, headers, "GET", "/friends")
  ).json();
  return friends.find((f: { username: string }) => f.username === username)
    .id as string;
}

const nudge = (
  request: APIRequestContext,
  headers: Headers,
  friendship: string,
  key = crypto.randomUUID(),
) =>
  call(request, headers, "POST", `/friends/${friendship}/nudge`, {
    operation_key: key,
  });

for (const mobile of [false, true]) {
  const label = mobile ? "mobile" : "desktop";
  const setup = async (page: Page, request: APIRequestContext) => {
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
  };
  const prefix = () => `n${Math.random().toString(36).slice(2, 6)}`;

  test(`nudge a friend once a day, retry safely, see the inbox ${label}`, async ({
    page,
    request,
  }) => {
    await setup(page, request);
    const p = prefix();
    await page.goto("/friends");
    const mine = await me(page, request, `${p}.mina`);
    const sam = await learner(request, `${p}.sam`);
    const kai = await learner(request, `${p}.kai`);
    const lena = await learner(request, `${p}.lena`);
    await befriend(request, sam, mine.headers, mine.name);
    await befriend(request, kai, mine.headers, mine.name);
    await befriend(request, lena, mine.headers, mine.name);
    await page.goto("/friends");

    const list = page.getByRole("list", { name: "Your friends" });
    const row = list.getByRole("listitem").filter({ hasText: `@${p}.sam` });
    const button = row.getByRole("button", { name: /^Nudge / });
    await expect(button).toBeVisible();
    await button.focus();
    await button.press("Enter");
    await expect(row).toContainText(
      "Nudge sent · you can nudge again tomorrow",
    );
    await expect(row.getByRole("button", { name: /^Nudge / })).toHaveCount(0);

    // Reload: the sent state comes from the server, in the list and the detail.
    await page.reload();
    await expect(
      list.getByRole("listitem").filter({ hasText: `@${p}.sam` }),
    ).toContainText("Nudge sent · you can nudge again tomorrow");
    await page
      .getByRole("button", { name: /^Open / })
      .filter({ hasText: `@${p}.sam` })
      .click();
    const detail = page.getByRole("region", { name: "Friend details" });
    await expect(detail).toContainText(
      "Nudge sent · you can nudge again tomorrow",
    );
    await expect(detail.getByRole("button", { name: /^Nudge / })).toHaveCount(
      0,
    );

    // API: a second nudge with a new key is limited; the same key is a replay.
    const id = await friendshipId(request, mine.headers, `${p}.sam`);
    const second = await nudge(request, mine.headers, id);
    expect(second.status()).toBe(429);
    expect((await second.json()).code).toBe("nudge_limited");
    expect(Number(second.headers()["retry-after"])).toBeGreaterThan(0);
    const inbox = await (await call(request, sam, "GET", "/nudges")).json();
    expect(inbox.nudges).toHaveLength(1);
    expect(inbox.unread).toBe(1);
    const key = crypto.randomUUID();
    const kaiId = await friendshipId(request, mine.headers, `${p}.kai`);
    const first = await (await nudge(request, mine.headers, kaiId, key)).json();
    const replay = await nudge(request, mine.headers, kaiId, key);
    expect(replay.status()).toBe(200);
    expect((await replay.json()).id).toBe(first.id);
    expect(
      (await (await call(request, kai, "GET", "/nudges")).json()).nudges,
    ).toHaveLength(1);

    // A limit answered by the server shows a calm message, no raw error.
    await page.goto("/friends");
    await page.route("**/v1/social/friends/*/nudge", (route) =>
      route.fulfill({
        status: 429,
        headers: { "retry-after": "3600" },
        json: { code: "nudge_limited" },
      }),
    );
    const lenaRow = page
      .getByRole("list", { name: "Your friends" })
      .getByRole("listitem")
      .filter({ hasText: `@${p}.lena` });
    await lenaRow.getByRole("button", { name: /^Nudge / }).click();
    await expect(page.getByRole("alert")).toContainText("You already nudged");
    await page.unroute("**/v1/social/friends/*/nudge");

    // Received nudges: the inbox tab counts, lists and clears on opening.
    expect(
      (
        await nudge(request, sam, await friendshipId(request, sam, mine.name))
      ).status(),
    ).toBe(200);
    await page.goto("/friends");
    const tab = page.getByRole("tab", { name: /^Nudges · 1$/ });
    await expect(tab).toBeVisible();
    await tab.focus();
    await tab.press("Enter");
    const received = page.getByRole("list", { name: "Nudges" });
    await expect(received).toContainText(`@${p}.sam`);
    await expect(received).toContainText("nudged you");
    await expect(page.getByRole("tab", { name: "Nudges · 0" })).toBeVisible();
  });

  test(`blocking stops delivery of nudges ${label}`, async ({
    page,
    request,
  }) => {
    await setup(page, request);
    const p = prefix();
    await page.goto("/friends");
    const mine = await me(page, request, `${p}.mina`);
    const sam = await learner(request, `${p}.sam`);
    await befriend(request, sam, mine.headers, mine.name);
    const samId = await friendshipId(request, sam, mine.name);
    expect((await nudge(request, sam, samId)).status()).toBe(200);
    await page.goto("/friends");
    await page.getByRole("tab", { name: /^Nudges · 1$/ }).click();
    await expect(page.getByRole("list", { name: "Nudges" })).toContainText(
      `@${p}.sam`,
    );

    await page.getByRole("tab", { name: /^Friends · / }).click();
    await page
      .getByRole("button", { name: /^Open / })
      .filter({ hasText: `@${p}.sam` })
      .click();
    await page.getByRole("button", { name: "Block", exact: true }).click();
    await page
      .getByRole("dialog", { name: /^Block .*\?$/ })
      .getByRole("button", { name: "Block", exact: true })
      .click();

    // The pending nudge is gone and a new one fails like an unknown friend.
    await page.getByRole("tab", { name: /^Nudges · / }).click();
    await expect(page.getByRole("tab", { name: "Nudges · 0" })).toBeVisible();
    await expect(page.getByText("No nudges right now.")).toBeVisible();
    const inbox = await (
      await call(request, mine.headers, "GET", "/nudges")
    ).json();
    expect(
      inbox.nudges.map((n: { username: string }) => n.username),
    ).not.toContain(`${p}.sam`);
    const blocked = await nudge(request, sam, samId);
    expect(blocked.status()).toBe(404);
    expect((await blocked.json()).code).toBe("nudge_unavailable");
    const unknown = await nudge(request, sam, crypto.randomUUID());
    expect(await blocked.json()).toEqual(await unknown.json());
  });

  test(`deleting a connected account leaves no trace ${label}`, async ({
    page,
    request,
  }) => {
    await setup(page, request);
    const p = prefix();
    await page.goto("/friends");
    const mine = await me(page, request, `${p}.mina`);
    const kai = await learner(request, `${p}.kai`);
    await befriend(request, kai, mine.headers, mine.name);
    const kaiId = await friendshipId(request, kai, mine.name);
    expect((await nudge(request, kai, kaiId)).status()).toBe(200);
    await page.goto("/friends");
    await expect(
      page.getByRole("list", { name: "Your friends" }),
    ).toContainText(`@${p}.kai`);
    await expect(page.getByRole("tab", { name: /^Nudges · 1$/ })).toBeVisible();

    const deleted = await request.post(`${apiBase}/v1/me/deletion-request`, {
      headers: kai,
      data: { confirmation: "DELETE" },
    });
    expect(deleted.status()).toBe(200);

    await page.reload();
    await expect(page.getByRole("heading", { name: "Friends" })).toBeVisible();
    await expect(page.locator("body")).not.toContainText(`${p}.kai`);
    await page.getByRole("tab", { name: /^Nudges · / }).click();
    await expect(page.getByText("No nudges right now.")).toBeVisible();
    await expect(page.locator("body")).not.toContainText(`${p}.kai`);
    const inbox = await (
      await call(request, mine.headers, "GET", "/nudges")
    ).json();
    expect(JSON.stringify(inbox)).not.toContain(`${p}.kai`);
    const friends = await (
      await call(request, mine.headers, "GET", "/friends")
    ).json();
    expect(JSON.stringify(friends)).not.toContain(`${p}.kai`);

    // The released name stays held: nobody else can take it yet.
    const other = await signIn(request);
    const claim = await request.put(`${apiBase}/v1/social/username`, {
      headers: other,
      data: { username: `${p}.kai` },
    });
    expect(claim.status()).toBe(409);
    expect((await claim.json()).code).toBe("username_taken");
  });
}
