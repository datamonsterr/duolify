import { test, expect } from "./native-store-fixture";
import { apiBase, resetAuthRateLimits } from "./servers";

test.use({ hasTouch: true });

test.beforeEach(async ({ page, request }) => {
  await resetAuthRateLimits(request);
  await page.route(/\/v1\/(auth\/|me(?:\/|$|\?))/, async (route) => {
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
  });
});

test("desktop setup chat: vague follow-up, live summary edit, confirm only once", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });

  await page.goto("/sign-in?returnTo=%2Fprofile%2Fsetup");
  await page.getByRole("button", { name: "Get started" }).tap();
  const email = `setup-chat-desktop@example.test`;
  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("I am 16 or older").check();
  await page.getByRole("button", { name: "Send code" }).tap();
  await expect(page.getByLabel("One-time code")).toBeVisible();
  const mail = await (
    await request.get(`${apiBase}/test/mail?email=${email}`)
  ).json();
  await page.getByLabel("One-time code").fill(mail.code);
  await page.getByRole("button", { name: "Sign in", exact: true }).tap();

  // Should arrive at setup chat
  await expect(
    page.getByRole("heading", { name: "Let's set things up" }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Prefer a form?" }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Skip setup" })).toBeVisible();

  // Assistant asks first question
  await expect(
    page.getByText("What would you like to get better at?"),
  ).toBeVisible();

  // Intercept profile writes to ensure no write occurs before confirmation
  let writes = 0;
  await page.route("**/v1/me/profile", async (route) => {
    if (route.request().method() === "POST") {
      writes++;
    }
    await route.fallback();
  });

  // Answer vaguely
  const composer = page.getByRole("textbox", { name: "Your answer" });
  await composer.fill("Something about space I guess");
  await page.getByRole("button", { name: "Send" }).tap();

  // Single follow-up question triggered
  await expect(
    page.getByText(
      "Space is a great pick! Is this for fun, for school, or for work?",
    ),
  ).toBeVisible();

  // Desktop live summary shows extracted topic
  await expect(page.getByText("What I've understood")).toBeVisible();
  expect(writes).toBe(0);

  // Answer the follow-up
  await composer.fill("Fun — I want to explain the night sky to my kids");
  await page.getByRole("button", { name: "Send" }).tap();

  // Moves to next step without a second follow-up
  await expect(
    page.getByText("What level would you describe yourself as?"),
  ).toBeVisible();
  await page.getByRole("button", { name: "Beginner" }).tap();

  // Next step: daily time
  await expect(
    page.getByText("How much time can you give it on a normal day?"),
  ).toBeVisible();
  await page.getByRole("button", { name: "10 min · Regular" }).tap();

  // Next step: reminders
  await expect(page.getByText("Would you like daily reminders?")).toBeVisible();
  await page.getByRole("button", { name: "Skip" }).tap();

  // Next step: time zone confirmation
  await expect(page.getByText("Does this time zone look right?")).toBeVisible();
  await page.getByRole("button", { name: "Looks right" }).tap();

  // Reaches confirmation step
  await expect(
    page.getByText("Here's what I've got. Does this look right?"),
  ).toBeVisible();
  expect(writes).toBe(0);

  // Edit a summary line (Topic -> Astronomy)
  await page.getByLabel("Edit topic").tap();
  const topicInput = page.getByRole("textbox", { name: "Topic" });
  await topicInput.fill("Astronomy");
  await page.getByRole("button", { name: "Apply" }).tap();

  // Confirm profile
  await page
    .getByRole("button", { name: "Looks good — plan my first course" })
    .tap();
  await expect(page.getByText("Setup complete")).toBeVisible();
  expect(writes).toBe(1);

  // Verify persisted profile
  await page.getByRole("link", { name: "Edit profile" }).tap();
  await expect(page.getByLabel("Topic", { exact: true })).toHaveValue(
    "Astronomy",
  );
  await expect(page.getByLabel("Level", { exact: true })).toHaveValue(
    "beginner",
  );
});

test("mobile setup chat: summary sheet, provider failure form recovery, skip", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });

  await page.goto("/sign-in?returnTo=%2Fprofile%2Fsetup");
  await page.getByRole("button", { name: "Get started" }).tap();
  const email = `setup-chat-mobile@example.test`;
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
    page.getByRole("heading", { name: "Let's set things up" }),
  ).toBeVisible();

  // Send first answer
  const composer = page.getByRole("textbox", { name: "Your answer" });
  await composer.fill("Astronomy");
  await page.getByRole("button", { name: "Send" }).tap();

  // Mobile: summary opens as a sheet via the Summary chip/button
  await page.getByRole("button", { name: "Summary" }).tap();
  const dialog = page.getByRole("dialog", { name: "What I've understood" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText("Astronomy", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "Close" }).tap();
  await expect(dialog).not.toBeVisible();

  // Simulate provider failure on next message
  await page.route("**/v1/me/setup-chat", async (route) => {
    await route.fulfill({
      status: 503,
      json: { error: "Assistant unavailable" },
    });
  });

  await composer.fill("Beginner");
  await page.getByRole("button", { name: "Send" }).tap();

  // Assistant failure triggers form fallback with answers intact
  await expect(page.getByText("The setup chat isn't responding")).toBeVisible();
  await expect(page.getByLabel("Topic", { exact: true })).toHaveValue(
    "Astronomy",
  );
  await expect(
    page.getByRole("button", { name: "Try chat again" }),
  ).toBeVisible();

  // Skip setup discards unconfirmed draft
  await page.getByRole("button", { name: "Skip setup" }).tap();
  await expect(page.getByText("Setup complete")).toBeVisible();
});

test("profile update by chat opens setup workflow with existing values", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });

  // Sign in and complete basic setup
  await page.goto("/sign-in?returnTo=%2Fprofile");
  await page.getByRole("button", { name: "Get started" }).tap();
  const email = `setup-chat-update@example.test`;
  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("I am 16 or older").check();
  await page.getByRole("button", { name: "Send code" }).tap();
  await expect(page.getByLabel("One-time code")).toBeVisible();
  const mail = await (
    await request.get(`${apiBase}/test/mail?email=${email}`)
  ).json();
  await page.getByLabel("One-time code").fill(mail.code);
  await page.getByRole("button", { name: "Sign in", exact: true }).tap();

  // Save initial profile through form
  await page.getByRole("link", { name: "Set up learner context" }).tap();
  await page.getByRole("button", { name: "Use form instead" }).tap();
  await page.getByLabel("Topic", { exact: true }).fill("Astronomy");
  await page.getByRole("button", { name: "Review summary" }).tap();
  await page.getByRole("button", { name: "Confirm profile" }).tap();
  await expect(page.getByText("Setup complete")).toBeVisible();

  // Go to profile
  await page.goto("/profile");
  await expect(page.getByText("Signed in as")).toBeVisible();

  // Open "Update by chat"
  await page.getByRole("button", { name: "Update by chat" }).tap();
  const chatDialog = page.getByRole("dialog", {
    name: "Update profile by chat",
  });
  await expect(chatDialog).toBeVisible();
  await expect(chatDialog.getByText("What I've understood")).toBeVisible();
  await expect(
    chatDialog.getByText("Astronomy", { exact: true }),
  ).toBeVisible();
});
