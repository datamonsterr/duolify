import { test, expect } from "./native-store-fixture";
import type { Page } from "@playwright/test";

// Sign-in as a guided chat: real inputs (autofill, OTP, password managers), the age gate,
// wrong code, resend, throttling and offline. The API is mocked; the full path is in e1-02.
async function start(page: Page) {
  await page.goto("/sign-in");
  await page.getByRole("button", { name: "Get started" }).click();
}
async function emailStep(page: Page, email = "chat@example.test") {
  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("Email address").press("Enter");
}

test("inputs carry autofill attributes for email and one-time codes", async ({
  page,
}) => {
  await page.route("**/v1/auth/code", (r) => r.fulfill({ status: 204 }));
  await start(page);
  const email = page.getByLabel("Email address");
  await expect(email).toHaveAttribute("type", "email");
  await expect(email).toHaveAttribute("autocomplete", "email");
  await expect(email).toHaveAttribute("inputmode", "email");
  await expect(email).toHaveAttribute("name", "email");
  await emailStep(page);
  await page.getByLabel("I am 16 or older").check();
  await page.getByRole("button", { name: "Send code" }).click();
  const code = page.getByLabel("One-time code");
  await expect(code).toHaveAttribute("autocomplete", "one-time-code");
  await expect(code).toHaveAttribute("inputmode", "numeric");
  await expect(code).toBeFocused();
});

test("age gate: no code is requested until the box is ticked", async ({
  page,
}) => {
  let requested = 0;
  await page.route("**/v1/auth/code", (r) => {
    requested++;
    return r.fulfill({ status: 204 });
  });
  await start(page);
  await emailStep(page);
  const send = page.getByRole("button", { name: "Send code" });
  await expect(send).toBeDisabled();
  expect(requested).toBe(0);
  await page.getByLabel("I am 16 or older").check();
  await expect(send).toBeEnabled();
  await send.click();
  await expect(page.getByLabel("One-time code")).toBeVisible();
  expect(requested).toBe(1);
});

test("wrong code keeps the step open, then resend asks for a fresh one", async ({
  page,
}) => {
  let requested = 0;
  await page.route("**/v1/auth/code", (r) => {
    requested++;
    return r.fulfill({ status: 204 });
  });
  await page.route("**/v1/auth/verify", (r) =>
    r.fulfill({
      status: 400,
      json: { error: "That code is invalid or expired." },
    }),
  );
  await start(page);
  await emailStep(page);
  await page.getByLabel("I am 16 or older").check();
  await page.getByRole("button", { name: "Send code" }).click();
  await page.getByLabel("One-time code").fill("000000");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("invalid or expired");
  await expect(page.getByLabel("One-time code")).toBeVisible();
  await page.getByRole("button", { name: "Resend code" }).click();
  await expect(page.getByRole("status")).toContainText("Code sent");
  expect(requested).toBe(2);
});

test("throttled requests show the server message and stay retryable", async ({
  page,
}) => {
  let calls = 0;
  await page.route("**/v1/auth/code", (r) => {
    calls++;
    return calls === 1
      ? r.fulfill({
          status: 429,
          json: { error: "Too many codes. Try again in a few minutes." },
        })
      : r.fulfill({ status: 204 });
  });
  await start(page);
  await emailStep(page);
  await page.getByLabel("I am 16 or older").check();
  await page.getByRole("button", { name: "Send code" }).click();
  await expect(page.getByRole("alert")).toContainText("Too many codes");
  await expect(page.getByRole("button", { name: "Send code" })).toBeEnabled();
  await page.getByRole("button", { name: "Send code" }).click();
  await expect(page.getByLabel("One-time code")).toBeVisible();
});

test("offline: the code is not requested and the answers stay", async ({
  page,
  context,
}) => {
  let requested = 0;
  await page.route("**/v1/auth/code", (r) => {
    requested++;
    return r.fulfill({ status: 204 });
  });
  await start(page);
  await emailStep(page);
  await page.getByLabel("I am 16 or older").check();
  await context.setOffline(true);
  await expect(page.getByText("You're offline").first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Send code" })).toBeDisabled();
  expect(requested).toBe(0);
  await context.setOffline(false);
  await expect(page.getByLabel("I am 16 or older")).toBeChecked();
  await page.getByRole("button", { name: "Send code" }).click();
  await expect(page.getByLabel("One-time code")).toBeVisible();
});

for (const [width, height] of [
  [390, 844],
  [844, 390],
  [1280, 800],
] as const) {
  test(`sign-in chat fits ${width}x${height} without sideways scroll`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height });
    await page.route("**/v1/auth/code", (r) => r.fulfill({ status: 204 }));
    await start(page);
    await expect(page.getByLabel("Email address")).toBeVisible();
    await page.screenshot({ path: info.outputPath(`${width}-email.png`) });
    await emailStep(page);
    await page.getByLabel("I am 16 or older").check();
    await page.screenshot({ path: info.outputPath(`${width}-age.png`) });
    await page.getByRole("button", { name: "Send code" }).click();
    await expect(page.getByLabel("One-time code")).toBeVisible();
    await page.screenshot({ path: info.outputPath(`${width}-code.png`) });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}
