import { test, expect } from "./accounts-fixture";
import { apiBase } from "./servers";

for (const width of [1280, 390, 320]) {
  test(`reserve, settle and recover eligible quotes at ${width}px`, async ({
    page,
    request,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    let quoteId = "";
    await page.route("**/v1/quotes**", async (route) => {
      const original = route.request();
      const headers = original.headers();
      delete headers.origin;
      const response = await request.fetch(
        apiBase + new URL(original.url()).pathname,
        {
          method: original.method(),
          headers,
          data: original.postData() ?? undefined,
        },
      );
      if (
        original.method() === "POST" &&
        new URL(original.url()).pathname === "/v1/quotes"
      ) {
        quoteId = (await response.json()).id;
      }
      await route.fulfill({ response });
    });
    await page.route("**/v1/pricing", async (route) =>
      route.fulfill({ response: await request.get(apiBase + "/v1/pricing") }),
    );
    await page.goto("/credits");
    await page.getByRole("button", { name: "Course creation costs" }).click();
    await page
      .getByRole("combobox", { name: "Action", exact: true })
      .selectOption("quick_note");
    await page.getByRole("button", { name: "Get action quote" }).click();
    await page.getByRole("button", { name: "Accept quote terms" }).click();
    await page.getByRole("button", { name: "Reserve credits" }).click();
    await expect(
      page.getByRole("status").filter({ hasText: "Credits reserved" }),
    ).toBeVisible();
    const tokens = await page.evaluate(() =>
      JSON.parse(sessionStorage.getItem("duolify.auth")!),
    );
    const headers = { Authorization: `Bearer ${tokens.access_token}` };
    const job = await page
      .getByTestId("reservation")
      .getAttribute("data-job-id");
    const repeated = await Promise.all(
      Array.from({ length: 4 }, () =>
        request.post(apiBase + `/v1/quotes/${quoteId}/reserve`, { headers }),
      ),
    );
    for (const response of repeated) {
      expect(response.status()).toBe(200);
      expect((await response.json()).id).toBe(job);
    }
    const competing = await request.post(apiBase + "/v1/quotes", {
      headers,
      data: { action: "quick_note" },
    });
    expect(competing.status()).toBe(200);
    const competingId = (await competing.json()).id;
    expect(
      (
        await request.post(apiBase + `/v1/quotes/${competingId}/accept`, {
          headers,
        })
      ).status(),
    ).toBe(200);
    const blocked = await request.post(
      apiBase + `/v1/quotes/${competingId}/reserve`,
      { headers },
    );
    expect(blocked.status()).toBe(409);
    expect((await blocked.json()).error).toBe("concurrency_limit");
    const result = await request.post(apiBase + `/test/credits/${job}/settle`, {
      headers,
    });
    expect(result.status()).toBe(200);
    expect((await result.json()).content_spent).toBe(2);
    // Replaying settlement cannot append another charge.
    expect(
      (
        await request.post(apiBase + `/test/credits/${job}/settle`, { headers })
      ).status(),
    ).toBe(200);
    await page.getByRole("button", { name: "Renew quote" }).click();
    await page
      .getByRole("combobox", { name: "Action", exact: true })
      .selectOption("preview");
    await page.getByRole("button", { name: "Get action quote" }).click();
    await page.getByRole("button", { name: "Accept quote terms" }).click();
    await page.getByRole("button", { name: "Reserve credits" }).click();
    await expect(page.getByRole("alert")).toContainText("Next daily grant");
    await page
      .getByRole("combobox", { name: "Action", exact: true })
      .selectOption("lesson_content");
    await page.getByRole("button", { name: "Get action quote" }).click();
    await page.getByRole("button", { name: "Accept quote terms" }).click();
    await page.getByRole("button", { name: "Reserve credits" }).click();
    await expect(page.getByRole("alert")).toContainText("Action limit reached");
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}
