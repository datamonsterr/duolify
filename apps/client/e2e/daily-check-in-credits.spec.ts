import { expect, test } from "./accounts-fixture";

test("daily attendance confirms first, next, partial and full check-ins once", async ({
  page,
}) => {
  let attended = false;
  let amount = 400;
  let bank = 400;
  let first = true;
  let boundary = Date.now() + 86_400_000;
  await page.route("**/v1/me/check-in", (route) => {
    const checked = !attended;
    attended = true;
    return route.fulfill({
      json: {
        granted: checked ? amount : 0,
        first_time: checked && first,
        balance: bank + 46,
        banked_checkin: bank,
        cap: 1050,
        checked_in: checked,
        next_check_in_at: new Date(boundary).toISOString(),
      },
    });
  });
  await page.goto("/");
  await expect(
    page.getByRole("status").filter({ hasText: "+400" }),
  ).toContainText("446");
  await page.reload();
  await expect(
    page.getByRole("status").filter({ hasText: "+400" }),
  ).toHaveCount(0);
  for (const next of [
    { amount: 150, bank: 550 },
    { amount: 1, bank: 1050 },
    { amount: 0, bank: 1050 },
  ]) {
    attended = false;
    first = false;
    amount = next.amount;
    bank = next.bank;
    boundary += 86_400_000;
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    const notice = page
      .getByRole("status")
      .filter({ hasText: amount ? `+${amount} Credits` : "bank is full" });
    await expect(notice).toContainText(
      new Intl.NumberFormat("en").format(bank + 46),
    );
    if (amount === 1) await expect(notice).toContainText("cap");
  }
});
