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
  const email = `e6-03-${crypto.randomUUID()}@example.test`;
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

for (const mobile of [false, true]) {
  test(`send, accept, decline, link and remove ${mobile ? "mobile" : "desktop"}`, async ({
    page,
    request,
    browser,
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
    await page
      .context()
      .grantPermissions(["clipboard-read", "clipboard-write"]);
    await proxySocial(page, request);
    const p = `t${Math.random().toString(36).slice(2, 6)}`;
    await page.goto("/friends");
    const mine = await me(page, request, `${p}.mina`);
    const sam = await learner(request, `${p}.sam`);
    const bea = await learner(request, `${p}.bea`);
    const lena = await learner(request, `${p}.lena`);
    const ana = await learner(request, `${p}.ana`);
    const dex = await learner(request, `${p}.dex`);
    const kai = await learner(request, `${p}.kai`);

    // Incoming requests and an existing friend arrive from other learners.
    for (const from of [sam, bea, lena])
      expect(
        (
          await call(request, from, "POST", "/friend-requests", {
            username: mine.name,
          })
        ).status(),
      ).toBe(200);
    const lenaReq = (
      await (
        await call(request, mine.headers, "GET", "/friend-requests")
      ).json()
    ).incoming.find((r: { username: string }) => r.username === `${p}.lena`);
    await call(
      request,
      mine.headers,
      "POST",
      `/friend-requests/${lenaReq.id}/accept`,
    );

    // Offline/error: the list explains and recovers.
    await page.route("**/v1/social/friends", (route) => route.abort());
    await page.goto("/friends");
    await expect(page.getByRole("alert")).toContainText(
      "Check your connection",
    );
    await page.unroute("**/v1/social/friends");
    await proxySocial(page, request);
    await page.getByRole("button", { name: "Retry" }).click();
    await expect(page.getByRole("tab", { name: /^Friends · / })).toBeVisible();

    // Requests: keyboard accept, touch-sized decline, neutral cooldown afterwards.
    await page.getByRole("tab", { name: /^Requests · / }).click();
    const incoming = page.getByRole("list", { name: "Friend requests" });
    await expect(incoming).toContainText(`@${p}.sam`);
    const accept = page.getByRole("button", { name: `Accept ${p}.sam` });
    await accept.focus();
    await accept.press("Enter");
    await expect(
      page.getByRole("button", { name: `Accept ${p}.sam` }),
    ).toHaveCount(0);
    await page.getByRole("button", { name: `Decline ${p}.bea` }).click();
    await expect(
      page.getByRole("button", { name: `Decline ${p}.bea` }),
    ).toHaveCount(0);
    const again = await call(request, bea, "POST", "/friend-requests", {
      username: mine.name,
    });
    expect(again.status()).toBe(404);
    expect((await again.json()).code).toBe("request_unavailable");

    // Friends list: sam is a friend; open details and remove with a confirm.
    await page.getByRole("tab", { name: /^Friends · / }).click();
    const friends = page.getByRole("list", { name: "Your friends" });
    await expect(friends).toContainText(`@${p}.sam`);
    await expect(friends).toContainText(`@${p}.lena`);
    await page
      .getByRole("button", { name: new RegExp(`^Open .*`) })
      .filter({ hasText: `@${p}.lena` })
      .click();
    const detail = page.getByRole("region", { name: "Friend details" });
    await expect(detail).toContainText(`@${p}.lena`);
    if (mobile) {
      await expect(friends).toBeHidden();
      await page.getByRole("button", { name: "Back to friends" }).click();
      await expect(friends).toBeVisible();
      await page
        .getByRole("button", { name: /^Open / })
        .filter({ hasText: `@${p}.lena` })
        .click();
    }
    const trigger = page.getByRole("button", { name: "Remove friend" });
    await trigger.focus();
    await trigger.press("Enter");
    const confirm = page.getByRole("dialog", { name: /^Remove .*\?$/ });
    await expect(confirm).toContainText("isn't notified");
    await page.keyboard.press("Escape");
    await expect(confirm).toBeHidden();
    await expect(trigger).toBeFocused();
    await trigger.press("Enter");
    await confirm.getByRole("button", { name: "Remove", exact: true }).click();
    await expect(friends).not.toContainText(`@${p}.lena`);
    const lenaFriends = await (
      await call(request, lena, "GET", "/friends")
    ).json();
    expect(lenaFriends.friends).toEqual([]);

    // Add by username: search, send, pending shows under Sent, then sender is accepted.
    await page.getByRole("button", { name: "Add", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "Add a friend" });
    await dialog.getByRole("textbox", { name: "Username" }).fill(`${p}.ana`);
    await dialog
      .getByRole("button", { name: `Send request to ${p}.ana` })
      .click();
    await expect(dialog).toContainText("Request sent");
    const anaIncoming = (
      await (await call(request, ana, "GET", "/friend-requests")).json()
    ).incoming;
    expect(anaIncoming).toHaveLength(1);
    await call(
      request,
      ana,
      "POST",
      `/friend-requests/${anaIncoming[0].id}/accept`,
    );

    // Owner link: copy, someone uses it, turn it off, the next use is refused.
    await dialog.getByRole("button", { name: "Copy invite link" }).click();
    await expect(dialog).toContainText("Link copied");
    await expect(dialog).toContainText("Valid for 30 days");
    const link = await dialog
      .getByRole("textbox", { name: "Invite link" })
      .inputValue();
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
      link,
    );
    const token = link.split("/friends/invite/")[1];
    expect(token).toHaveLength(43);
    expect(
      (
        await call(request, dex, "POST", "/friend-requests", { token })
      ).status(),
    ).toBe(200);
    await dialog.getByRole("button", { name: "Turn off link" }).click();
    await expect(
      dialog.getByRole("button", { name: "Copy invite link" }),
    ).toBeVisible();
    expect(
      (
        await call(request, kai, "POST", "/friend-requests", { token })
      ).status(),
    ).toBe(404);
    await dialog.getByRole("button", { name: "Close" }).click();

    // Guest link: signed-in landing previews, then needs an explicit confirm.
    const kaiLink = await (
      await call(request, kai, "POST", "/friend-link")
    ).json();
    await page.goto(`/friends/invite/${kaiLink.token}`);
    await expect(
      page.getByRole("heading", { name: "Friend invite" }),
    ).toBeVisible();
    await expect(page.getByRole("main")).toContainText(`@${p}.kai`);
    expect(
      (await (await call(request, kai, "GET", "/friend-requests")).json())
        .incoming,
    ).toEqual([]);
    await page.getByRole("button", { name: "Send friend request" }).click();
    await expect(page.getByRole("main")).toContainText("Request sent");
    expect(
      (await (await call(request, kai, "GET", "/friend-requests")).json())
        .incoming,
    ).toHaveLength(1);

    // Expired link: the neutral message, no way to send.
    await call(request, kai, "DELETE", "/friend-link");
    await page.goto(`/friends/invite/${kaiLink.token}`);
    await expect(page.getByRole("alert")).toContainText(
      "This invite has expired. Ask for a new one.",
    );
    await expect(
      page.getByRole("button", { name: "Send friend request" }),
    ).toHaveCount(0);

    // Signed out: the link reauthorizes through sign-in and creates nothing.
    const fresh = await call(request, dex, "POST", "/friend-link");
    const token2 = (await fresh.json()).token;
    const anonymous = await browser.newContext({
      baseURL: page.url().split("/friends")[0],
    });
    const anonPage = await anonymous.newPage();
    await anonPage.goto(`/friends/invite/${token2}`);
    await expect(anonPage).toHaveURL(
      /\/sign-in\?returnTo=%2Ffriends%2Finvite%2F/,
    );
    expect(
      (await (await call(request, dex, "GET", "/friend-requests")).json())
        .incoming.length,
    ).toBe(0);
    await anonymous.close();
  });
}
