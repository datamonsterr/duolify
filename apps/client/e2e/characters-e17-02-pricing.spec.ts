import { resetTokens, test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";

// ADR 0013 approves pricing only. Default-cast art remains provisional.
test.beforeEach(() => resetTokens());
test("E17-02 approved active pricing enables creation and excludes daily-free credits", async ({
  page,
  request,
}) => {
  await page.goto("/settings/characters");
  const token = await page.evaluate(
    () =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!)
        .access_token as string,
  );
  const headers = { Authorization: `Bearer ${token}` };
  await page.route(/\/v1\/characters/, async (route) => {
    const url = new URL(route.request().url());
    url.host = new URL(apiBase).host;
    await route.fulfill({
      response: await request.fetch(url.href, {
        method: route.request().method(),
        headers,
        data: route.request().postData() ?? undefined,
      }),
    });
  });
  await page.reload();
  const availability = () =>
    request.get(`${apiBase}/v1/characters/creation`, { headers });
  expect((await (await availability()).json()).enabled).toBe(false);
  await expect(
    page.getByRole("button", { name: "Review character quote" }),
  ).toBeDisabled();
  const activate = await request.post(
    `${apiBase}/test/characters/approve-price`,
    { headers },
  );
  expect(activate.status(), await activate.text()).toBe(204);
  expect(await (await availability()).json()).toMatchObject({
    enabled: true,
    proposal: false,
  });
  const activePricing = await (
    await request.get(`${apiBase}/v1/pricing`)
  ).json();
  expect(activePricing).toMatchObject({
    version: "usd-character-create-2026-11-16",
    design_chat_decision: "owner_approved",
    character_create_decision: "owner_approved",
    design_chat_policy: {
      setup_chat: "platform_paid",
      course_brief_chat: "platform_paid",
      outline_chat_change: "valid_delivered_proposal_even_if_discarded",
      undelivered_proposal: "release_reservation",
      manual_edits_apply_discard_undo: "free",
    },
    actions: {
      setup_chat: { credits: 0 },
      course_brief_chat: { credits: 0 },
      outline_chat_change: { credits: 5 },
      outline_replan: { credits: 55 },
      character_create: { credits: 25 },
    },
  });
  expect(activePricing.daily_free.eligible_actions).not.toContain(
    "character_create",
  );
  await page.reload();
  await page
    .getByLabel("Describe your original character")
    .fill("An original moon gardener");
  const quoted = page.waitForResponse(
    (r) =>
      r.url().endsWith("/v1/characters/jobs") &&
      r.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Review character quote" }).click();
  const job = await (await quoted).json();
  expect(job.quote).toMatchObject({
    pricing_version: "usd-character-create-2026-11-16",
    content_ceiling: 25,
    daily_free_eligible: false,
  });
  await expect(
    page.getByText(/Pricing version: usd-character-create-2026-11-16/),
  ).toBeVisible();
  await page.getByRole("button", { name: "Confirm character credits" }).click();
  await expect(page.getByRole("alert")).toContainText("insufficient_credits");
  const wallet = await (
    await request.get(`${apiBase}/v1/credits/wallet`, { headers })
  ).json();
  expect(wallet.daily_free.available).toBeGreaterThanOrEqual(25);
  expect(wallet.reserved).toBe(0);
});
