import { test, expect } from "@playwright/test";
import { apiBase, resetAuthRateLimits } from "./servers";

test.use({ hasTouch: true });
for (const mobile of [false, true]) {
  test(`course brief ${mobile ? "mobile" : "desktop"}: refine, confirm, invalidate and recover`, async ({
    page,
    request,
  }) => {
    await resetAuthRateLimits(request);
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    );
    await page.emulateMedia({
      colorScheme: mobile ? "dark" : "light",
      reducedMotion: "reduce",
    });
    let paidCalls = 0;
    await page.route(
      /\/v1\/(?:auth\/|me(?:\/|$|\?)|course-brief)/,
      async (route) => {
        const original = route.request();
        const target = new URL(original.url());
        target.host = new URL(apiBase).host;
        const headers = original.headers();
        delete headers.origin;
        await route.fulfill({
          response: await request.fetch(target.href, {
            method: original.method(),
            headers,
            data: original.postData() ?? undefined,
          }),
        });
      },
    );
    await page.route(
      /\/v1\/(?:courses\/(?:preview|outline)|credits\/reserve)/,
      async (route) => {
        paidCalls++;
        await route.fulfill({ status: 500 });
      },
    );
    await page.goto("/sign-in?returnTo=%2Fcourses%2Fcreate");
    await page.getByRole("button", { name: "Get started" }).tap();
    const email = `brief-${mobile}@example.test`;
    await page.getByLabel("Email address").fill(email);
    await page.getByLabel("I am 16 or older").check();
    await page.getByRole("button", { name: "Send code" }).tap();
    await expect(page.getByLabel("One-time code")).toBeVisible();
    const mail = await (
      await request.get(`${apiBase}/test/mail?email=${email}`)
    ).json();
    await page.getByLabel("One-time code").fill(mail.code);
    await page.getByRole("button", { name: "Sign in", exact: true }).tap();
    await expect(
      page.getByRole("heading", { name: "Create a course" }),
    ).toBeVisible();
    const composer = page.getByRole("textbox", { name: "Your answer" });
    await composer.fill("Astronomy");
    await page.getByRole("button", { name: "Send answer", exact: true }).tap();
    await expect(
      page.getByText("What is your goal?", { exact: true }),
    ).toBeVisible();
    await page.getByRole("link", { name: "Prefer a form?" }).tap();
    await page
      .getByLabel("Include topics", { exact: true })
      .fill("Moon phases; planets");
    await page.getByLabel("Skip topics", { exact: true }).fill("History");
    await page.getByLabel("Maths", { exact: true }).fill("Light maths");
    await page
      .getByRole("button", { name: "Confirm brief", exact: true })
      .tap();
    await expect(
      page.getByRole("status").filter({ hasText: "Brief confirmed" }),
    ).toBeVisible();
    await page.getByLabel("Maths", { exact: true }).fill("No maths");
    await expect(
      page.getByText("Brief confirmed", { exact: false }),
    ).toHaveCount(0);
    // Rotate an expired access token on return without losing the private draft.
    const credentials = await page.evaluate(() =>
      JSON.parse(sessionStorage.getItem("duolify.auth")!),
    );
    const expired = await request.post(`${apiBase}/test/expire-access`, {
      data: { access_token: credentials.access_token },
    });
    expect(expired.ok()).toBe(true);
    credentials.access_token = (await expired.json()).access_token;
    await page.evaluate(
      (tokens) =>
        sessionStorage.setItem("duolify.auth", JSON.stringify(tokens)),
      credentials,
    );
    await page.goto("/credits");
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            JSON.parse(sessionStorage.getItem("duolify.auth") || "null")
              ?.refresh_token,
        ),
      )
      .not.toBe(credentials.refresh_token);
    await page.goto("/courses/create");
    await expect(page.getByLabel("Maths", { exact: true })).toHaveValue(
      "No maths",
    );
    await expect(
      page.getByLabel("Include topics", { exact: true }),
    ).toHaveValue("Moon phases; planets");
    await page.getByRole("button", { name: "Keep chatting" }).tap();
    if (mobile) {
      await page.getByRole("button", { name: "Open course brief" }).tap();
      const sheet = page.getByRole("dialog", { name: "Course brief" });
      await expect(
        sheet.getByLabel("Skip topics", { exact: true }),
      ).toHaveValue("History");
      await sheet
        .getByRole("button", { name: "Close Course brief", exact: true })
        .tap();
    }
    await page.route("**/v1/course-brief/chat", (route) =>
      route.fulfill({ status: 503, json: { error: "unavailable" } }),
    );
    await composer.fill("Explain the sky to my kids");
    await page.getByRole("button", { name: "Send answer", exact: true }).tap();
    await expect(page.getByRole("alert")).toContainText(
      "Your answers are kept",
    );
    await expect(page.getByLabel("Goal", { exact: true })).toHaveValue(
      "Explain the sky to my kids",
    );
    expect(paidCalls).toBe(0);
    expect(page.url()).not.toContain("Moon");
  });
}
