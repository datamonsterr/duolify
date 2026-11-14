import type { APIRequestContext, Page } from "@playwright/test";
import { apiBase } from "./servers";
import { test, expect } from "./accounts-fixture";

// E15-06 course invites, real API. The browser learner is the owner in the
// "send" tests and the recipient in the "inbox" tests.
type Headers = { Authorization: string };

const prefix = () => `iv${crypto.randomUUID().replaceAll("-", "").slice(0, 8)}`;

async function signIn(request: APIRequestContext): Promise<Headers> {
  const email = `e15-06-${crypto.randomUUID()}@example.test`;
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

const social = (
  request: APIRequestContext,
  headers: Headers,
  method: string,
  path: string,
  data?: unknown,
) => request.fetch(`${apiBase}/v1/social${path}`, { method, headers, data });

async function learner(request: APIRequestContext, name: string) {
  const headers = await signIn(request);
  const named = await social(request, headers, "PUT", "/username", {
    username: name,
  });
  expect(named.status()).toBe(200);
  return headers;
}

/** The browser's own learner: token and username (a name is set on first use). */
async function me(page: Page, request: APIRequestContext, fallback: string) {
  const token = await page.evaluate(
    () =>
      JSON.parse(sessionStorage.getItem("duolify.native-store-fixture") ?? "{}")
        .access_token as string,
  );
  const headers = { Authorization: `Bearer ${token}` };
  const identity = await (
    await social(request, headers, "GET", "/identity")
  ).json();
  if (!identity.username) {
    await social(request, headers, "PUT", "/username", { username: fallback });
    return { headers, name: fallback };
  }
  return { headers, name: identity.username.username as string };
}

async function befriend(
  request: APIRequestContext,
  from: Headers,
  to: Headers,
  toName: string,
) {
  const sent = await (
    await social(request, from, "POST", "/friend-requests", {
      username: toName,
    })
  ).json();
  expect(
    (
      await social(request, to, "POST", `/friend-requests/${sent.id}/accept`)
    ).status(),
  ).toBe(200);
}

async function seedCourse(
  request: APIRequestContext,
  headers: Headers,
  title?: string,
) {
  const response = await request.post(`${apiBase}/test/marketplace/course`, {
    headers,
    data: {
      // Numeric UUID groups can look like phone numbers to publication screening.
      // Course IDs already isolate scenarios; keep learner-facing fixture text safe.
      title: title ?? "Invite course",
      description: "Small steps.",
    },
  });
  expect(response.status()).toBe(201);
  return (await response.json()).course_id as string;
}

/** Screens and links the course through the real routes, as the Share panel does. */
async function share(
  request: APIRequestContext,
  headers: Headers,
  course: string,
) {
  const draft = await request.post(
    `${apiBase}/v1/courses/${course}/publication-drafts`,
    { headers, data: {} },
  );
  expect((await draft.json()).verdict).toBe("pass");
  const link = await request.post(
    `${apiBase}/v1/courses/${course}/share-link`,
    { headers, data: {} },
  );
  expect(link.status()).toBeLessThan(300);
}

const invite = (
  request: APIRequestContext,
  headers: Headers,
  course: string,
  data: unknown,
) => request.post(`${apiBase}/v1/courses/${course}/invites`, { headers, data });

/** The web build talks to its own API origin; the disposable API listens elsewhere. */
async function forward(page: Page, request: APIRequestContext) {
  await page.route("**/v1/**", async (route) => {
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

async function openShare(page: Page, course: string) {
  await page.goto(`/course/${course}`);
  await page.getByTestId("share-course").click();
  const panel = page.getByTestId("share-course-panel");
  await expect(panel).toBeVisible();
  return panel;
}

async function noOverflow(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth + 1,
  );
  expect(overflow).toBe(false);
}

for (const mobile of [false, true]) {
  const label = mobile ? "mobile" : "desktop";
  test.describe(label, () => {
    test.use({ hasTouch: mobile });
    const tap = (locator: ReturnType<Page["locator"]>) =>
      mobile ? locator.tap() : locator.click();
    const setup = async (page: Page, request: APIRequestContext) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 780 } : { width: 1280, height: 860 },
      );
      await forward(page, request);
      await page.goto("/profile");
    };

    test(`owner publishes, invites by username and from the friends list, then revokes (${label})`, async ({
      page,
      request,
    }) => {
      await setup(page, request);
      const p = prefix();
      const mine = await me(page, request, `${p}.owner`);
      const sam = await learner(request, `${p}.sam`);
      const kai = await learner(request, `${p}.kai`);
      await befriend(request, sam, mine.headers, mine.name);
      await befriend(request, kai, mine.headers, mine.name);
      const course = await seedCourse(request, mine.headers);

      const panel = await openShare(page, course);
      // Private: the owner is asked to share by link first, never invited blindly.
      const prompt = panel.getByTestId("invite-private-prompt");
      await expect(prompt).toContainText("Anyone with the link");
      await expect(
        panel.getByRole("button", { name: "Send invite" }),
      ).toHaveCount(0);

      await tap(panel.locator("label", { hasText: "Anyone with the link" }));
      await expect(panel.getByTestId("share-link-url")).toBeVisible();
      await expect(prompt).toHaveCount(0);

      // By username.
      const field = panel.getByLabel("Friend's username");
      await field.fill(`@${p}.sam`);
      await tap(panel.getByRole("button", { name: "Send invite" }));
      await expect(panel.getByTestId("invite-notice")).toHaveText(
        "Invite sent",
      );
      const sent = panel.getByRole("list", { name: "Sent invites" });
      await expect(sent.getByTestId(`sent-invite-${p}.sam`)).toContainText(
        "Pending",
      );
      await expect(field).toHaveValue("");

      // Duplicate for the same recipient and course is neutral, not a second row.
      await field.fill(`${p}.sam`);
      await tap(panel.getByRole("button", { name: "Send invite" }));
      await expect(panel.getByRole("alert")).toContainText(
        "already have a pending invite",
      );
      await expect(sent.getByRole("listitem")).toHaveCount(1);

      // From the friend list.
      await tap(panel.getByTestId(`invite-friend-${p}.kai`));
      await expect(sent.getByTestId(`sent-invite-${p}.kai`)).toContainText(
        "Pending",
      );
      await expect(panel.getByTestId(`invite-friend-${p}.kai`)).toBeDisabled();

      // Reload keeps the server state; revoke from the sent list.
      await page.reload();
      await page.getByTestId("share-course").click();
      const again = page.getByTestId("share-course-panel");
      await expect(again.getByTestId(`sent-invite-${p}.sam`)).toBeVisible();
      await tap(
        again.getByRole("button", { name: `Revoke invite for @${p}.sam` }),
      );
      await expect(again.getByTestId(`sent-invite-${p}.sam`)).toContainText(
        "Revoked",
      );
      await expect(
        again.getByRole("button", { name: `Revoke invite for @${p}.sam` }),
      ).toHaveCount(0);
      await noOverflow(page);
    });

    test(`recipient sees the inbox, accepts once and declines (${label})`, async ({
      page,
      request,
    }) => {
      await setup(page, request);
      const p = prefix();
      const mine = await me(page, request, `${p}.mina`);
      const owner = await learner(request, `${p}.owner`);
      await befriend(request, owner, mine.headers, mine.name);
      const titleA = `Inbox course ${p} A`;
      const titleB = `Inbox course ${p} B`;
      const a = await seedCourse(request, owner, titleA);
      const b = await seedCourse(request, owner, titleB);
      await share(request, owner, a);
      await share(request, owner, b);
      const first = await invite(request, owner, a, { username: mine.name });
      expect(first.status()).toBe(201);
      const second = await invite(request, owner, b, { username: mine.name });
      expect(second.status()).toBe(201);

      await page.goto("/friends");
      const tab = page.getByRole("tab", { name: /^Invites · 2/ });
      await expect(tab).toBeVisible();
      await tap(tab);
      const list = page.getByRole("list", { name: "Course invites" });
      const rowA = list.getByTestId(`received-invite-${p}.owner-${titleA}`);
      await expect(rowA).toContainText(titleA);
      await expect(rowA).toContainText(`@${p}.owner`);
      await expect(
        page.getByRole("tab", { name: /^Invites · 0/ }),
      ).toBeVisible();

      // Decline B: it leaves the list, a repeat is not offered.
      const rowB = list.getByTestId(`received-invite-${p}.owner-${titleB}`);
      await tap(rowB.getByRole("button", { name: "Decline" }));
      await expect(rowB).toHaveCount(0);

      // Accept A: lands on the new pinned copy. A replay returns the same copy.
      await tap(rowA.getByRole("button", { name: "Accept" }));
      await expect(page).toHaveURL(/\/course\/[0-9a-f-]{36}$/);
      const copy = page.url().split("/course/")[1];
      expect(copy).not.toBe(a);
      const id = (await first.json()).id as string;
      const replay = await request.post(`${apiBase}/v1/invites/${id}/accept`, {
        headers: mine.headers,
      });
      expect(replay.status()).toBe(200);
      expect((await replay.json()).course_id).toBe(copy);
      const mineCourses = await (
        await request.get(`${apiBase}/v1/courses`, { headers: mine.headers })
      ).json();
      const titles = JSON.stringify(mineCourses);
      expect(titles.split(titleA).length - 1).toBe(1);

      await page.goto("/friends");
      await expect(
        page.getByRole("tab", { name: /^Invites · 0/ }),
      ).toBeVisible();
      await noOverflow(page);
    });

    test(`neutral outcomes: unknown name, hidden recipient, offline, rate limit (${label})`, async ({
      page,
      request,
    }) => {
      await setup(page, request);
      const p = prefix();
      const mine = await me(page, request, `${p}.owner2`);
      const quiet = await learner(request, `${p}.quiet`);
      await befriend(request, quiet, mine.headers, mine.name);
      expect(
        (
          await social(request, quiet, "PATCH", "/settings", {
            course_invites_from: "nobody",
          })
        ).status(),
      ).toBe(200);
      const course = await seedCourse(request, mine.headers);
      await share(request, mine.headers, course);

      const panel = await openShare(page, course);
      const field = panel.getByLabel("Friend's username");
      const send = panel.getByRole("button", { name: "Send invite" });

      // Unknown name: neutral failure, no hint whether the name exists.
      await field.fill(`${p}.ghost`);
      await tap(send);
      await expect(panel.getByRole("alert")).toHaveText(
        "We couldn't send that invite.",
      );

      // Recipient who accepts invites from nobody: the sender still sees "sent".
      await field.fill(`${p}.quiet`);
      await tap(send);
      await expect(panel.getByTestId("invite-notice")).toHaveText(
        "Invite sent",
      );
      await expect(panel.getByTestId(`sent-invite-${p}.quiet`)).toContainText(
        "Pending",
      );
      expect(
        (
          await (
            await request.get(`${apiBase}/v1/invites/received`, {
              headers: quiet,
            })
          ).json()
        ).invites,
      ).toHaveLength(0);

      // Rate limit and offline come back as calm, retryable messages.
      await page.route("**/v1/courses/*/invites", (route) =>
        route.request().method() === "POST"
          ? route.fulfill({
              status: 429,
              headers: { "Retry-After": "3600" },
              contentType: "application/json",
              body: JSON.stringify({ error: { code: "invite_rate_limited" } }),
            })
          : route.fallback(),
      );
      await field.fill(`${p}.other`);
      await tap(send);
      await expect(panel.getByRole("alert")).toContainText(
        "Daily invite limit reached",
      );
      await page.unroute("**/v1/courses/*/invites");
      await page.route("**/v1/courses/*/invites", (route) =>
        route.request().method() === "POST" ? route.abort() : route.fallback(),
      );
      await tap(send);
      await expect(panel.getByRole("alert")).toContainText(
        "Check your connection",
      );
      await noOverflow(page);
    });
  });
}

test("keyboard: panel traps focus and invites send with Enter", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 1280, height: 860 });
  await forward(page, request);
  await page.goto("/profile");
  const p = prefix();
  const mine = await me(page, request, `${p}.kb`);
  const sam = await learner(request, `${p}.sam`);
  await befriend(request, sam, mine.headers, mine.name);
  const course = await seedCourse(request, mine.headers);
  await share(request, mine.headers, course);

  const panel = await openShare(page, course);
  const field = panel.getByLabel("Friend's username");
  await field.focus();
  await field.fill(`${p}.sam`);
  await field.press("Enter");
  await expect(panel.getByTestId("invite-notice")).toHaveText("Invite sent");
  await page.keyboard.press("Escape");
  await expect(page.getByTestId("share-course-panel")).toHaveCount(0);
  await expect(page.getByTestId("share-course")).toBeFocused();
});

test("private course: send is refused and the prompt links to sharing", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 390, height: 780 });
  await forward(page, request);
  await page.goto("/profile");
  const p = prefix();
  const mine = await me(page, request, `${p}.priv`);
  const course = await seedCourse(request, mine.headers);
  const refused = await invite(request, mine.headers, course, {
    username: `${p}.anyone`,
  });
  expect(refused.status()).toBe(409);
  expect((await refused.json()).error).toBe("course_not_shared");
  const panel = await openShare(page, course);
  await expect(panel.getByTestId("invite-private-prompt")).toBeVisible();
});

test("dark theme and reduced motion keep the sheet usable", async ({
  page,
  request,
}) => {
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.setViewportSize({ width: 390, height: 780 });
  await forward(page, request);
  await page.goto("/profile");
  const p = prefix();
  const mine = await me(page, request, `${p}.dark`);
  const course = await seedCourse(request, mine.headers);
  await share(request, mine.headers, course);
  const panel = await openShare(page, course);
  await expect(panel.getByTestId("invite-section")).toBeVisible();
  const motion = await panel.evaluate((node) => {
    const style = getComputedStyle(node);
    return style.animationName === "none" || style.animationDuration === "0s";
  });
  expect(motion).toBe(true);
  await noOverflow(page);
});
