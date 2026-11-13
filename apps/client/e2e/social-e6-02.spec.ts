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

async function learner(
  request: APIRequestContext,
  name: string,
  settings: Record<string, unknown> = {},
) {
  const email = `e6-02-${crypto.randomUUID()}@example.test`;
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
  const headers = { Authorization: `Bearer ${token}` };
  const claimed = await request.put(`${apiBase}/v1/social/username`, {
    headers,
    data: { username: name },
  });
  expect(claimed.status()).toBe(200);
  if (Object.keys(settings).length)
    expect(
      (
        await request.patch(`${apiBase}/v1/social/settings`, {
          headers,
          data: settings,
        })
      ).status(),
    ).toBe(200);
  return headers;
}

for (const mobile of [false, true]) {
  test(`rename then search with discovery disabled ${mobile ? "mobile" : "desktop"}`, async ({
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
    const anna = await learner(request, `${p}.anna`);
    await learner(request, `${p}.annie`);
    await learner(request, `${p}.quiet`, {
      discoverable: false,
      request_policy: "everyone",
    });
    await learner(request, `${p}.ghost`, {
      discoverable: false,
      request_policy: "nobody",
    });

    // First claim through the existing form.
    await page.goto("/friends");
    await page.getByRole("textbox", { name: "Username" }).fill(`${p}.mina`);
    await page.getByRole("button", { name: "Claim username" }).click();
    await expect(
      page.getByRole("region", { name: "Your public identity" }),
    ).toContainText(`@${p}.mina`);

    // Add a friend: keyboard-first bounded search with neutral outcomes.
    const add = page.getByRole("button", { name: "Add", exact: true });
    await add.press("Enter");
    const dialog = page.getByRole("dialog", { name: "Add a friend" });
    await expect(dialog).toBeVisible();
    const search = dialog.getByRole("textbox", { name: "Username" });
    await expect(search).toBeFocused();
    await search.fill("tz");
    await expect(dialog).toContainText("At least 3 characters");
    await search.fill(`${p}.an`);
    const results = dialog.getByRole("list", { name: "Search results" });
    await expect(results.getByRole("listitem")).toHaveCount(2);
    await expect(results).toContainText(`@${p}.anna`);
    await expect(results).not.toContainText("@@");
    // Nondiscoverable: absent by prefix, found exactly when requests are open.
    await search.fill(`${p}.q`);
    await expect(dialog).toContainText("No one found with that username");
    await expect(dialog).toContainText("Check the spelling");
    await search.fill(`${p}.quiet`);
    await expect(results.getByRole("listitem")).toHaveCount(1);
    // Requests restricted looks exactly like a nonexistent name.
    await search.fill(`${p}.ghost`);
    await expect(dialog).toContainText("No one found with that username");
    const ghostText = await dialog.innerText();
    await search.fill(`${p}.nobodyhere`);
    await expect(dialog).toContainText("No one found with that username");
    expect(await dialog.innerText()).toBe(ghostText);
    // Error state offers recovery.
    let outage = true;
    await page.route("**/v1/social/search**", (r) =>
      outage
        ? r.fulfill({ status: 503, json: { error: "Try again." } })
        : r.fallback(),
    );
    await search.fill(`${p}.ann`);
    await expect(dialog.getByRole("alert")).toContainText("unavailable");
    outage = false;
    await dialog.getByRole("button", { name: "Retry" }).click();
    await expect(results.getByRole("listitem")).toHaveCount(2);
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(add).toBeFocused();

    // Settings: rename, cooldown, hold.
    await page.goto("/settings");
    await page.getByRole("button", { name: "Change username" }).click();
    const rename = page.getByRole("textbox", { name: "New username" });
    await rename.fill(`${p}.mina2`);
    await rename.press("Enter");
    await expect(page.getByText(`@${p}.mina2`)).toBeVisible();
    await page.getByRole("button", { name: "Change username" }).click();
    await rename.fill(`${p}.mina3`);
    await rename.press("Enter");
    await expect(
      page.getByRole("form", { name: "Change username" }).getByRole("alert"),
    ).toContainText("You can change your username again on");
    const held = await request.put(`${apiBase}/v1/social/username`, {
      headers: anna,
      data: { username: `${p}.mina` },
    });
    expect(held.status()).toBe(409);
    expect((await held.json()).code).toBe("username_already_set");
    const heldRename = await request.patch(`${apiBase}/v1/social/username`, {
      headers: anna,
      data: { username: `${p}.mina` },
    });
    expect(heldRename.status()).toBe(409);
    expect((await heldRename.json()).code).toBe("username_taken");

    // Privacy: failed save keeps the previous value; success hides from prefix search.
    const findable = page.getByRole("checkbox", {
      name: "Findable by username",
    });
    await expect(findable).toBeChecked();
    await page.route("**/v1/social/settings", (r) =>
      r.request().method() === "PATCH"
        ? r.fulfill({ status: 503, json: { error: "Try again." } })
        : r.fallback(),
    );
    await findable.press("Space");
    await expect(
      page.getByRole("region", { name: "Privacy & social" }).getByRole("alert"),
    ).toContainText("Couldn't save");
    await expect(findable).toBeChecked();
    await page.unroute("**/v1/social/settings");
    await findable.press("Space");
    await expect(findable).not.toBeChecked();
    await expect(
      page.getByRole("status", { name: "Privacy settings status" }),
    ).toContainText("Saved");
    const found = await request.get(
      `${apiBase}/v1/social/search?q=${p}.mina2`,
      { headers: anna },
    );
    expect((await found.json()).results).toHaveLength(1); // exact still works: requests open
    const prefix = await request.get(`${apiBase}/v1/social/search?q=${p}.min`, {
      headers: anna,
    });
    expect((await prefix.json()).results).toHaveLength(0);
    await page
      .getByRole("combobox", { name: "Requests and invites from" })
      .selectOption("nobody");
    await expect(
      page.getByRole("status", { name: "Privacy settings status" }),
    ).toContainText("Saved");
    const gone = await request.get(`${apiBase}/v1/social/search?q=${p}.mina2`, {
      headers: anna,
    });
    expect((await gone.json()).results).toHaveLength(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  });
}
