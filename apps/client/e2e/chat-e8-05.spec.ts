import type { APIRequestContext, Page } from "@playwright/test";
import { test, expect, resetTokens } from "./accounts-fixture";
import { apiBase } from "./servers";

resetTokens();
test.afterEach(() => resetTokens());

type State = {
  /** Message ids a save was attempted for; the mock server dedupes them like the API. */
  saveRequests: string[];
  notes: Set<string>;
  deletes: string[];
  failSaveOnce: boolean;
  failDelete: boolean;
};

const QUESTION = "Why does the North Star stay still?";
const REPLY =
  "Polaris sits almost exactly above Earth's north pole, so the star seems fixed.";

async function mockThread(page: Page, opts: Partial<State> = {}) {
  const state: State = {
    saveRequests: [],
    notes: new Set(),
    deletes: [],
    failSaveOnce: false,
    failDelete: false,
    ...opts,
  };
  let deleted = false;
  await page.route("**/v1/me/credits", (route) =>
    route.fulfill({
      json: {
        available: 0,
        daily_free: {
          available: 0,
          granted: 30,
          resets_at: "2030-01-02T00:00:00Z",
          timezone: "UTC",
        },
        purchased: 0,
        subscription: 0,
        reserved: 0,
        recent_activity: [],
      },
    }),
  );
  await page.route("**/v1/tutor/messages/*/note", async (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-2)!;
    state.saveRequests.push(id);
    if (state.failSaveOnce) {
      state.failSaveOnce = false;
      state.notes.add(id); // the server committed; only the response was lost
      return route.abort("connectionreset");
    }
    state.notes.add(id);
    return route.fulfill({
      json: { id: "note-1", status: "enrichment_pending", truncated: false },
    });
  });
  await page.route("**/v1/tutor/threads/*", async (route) => {
    if (route.request().method() !== "DELETE") return route.continue();
    state.deletes.push(new URL(route.request().url()).pathname);
    if (state.failDelete) {
      return route.fulfill({ status: 503, json: { error: "unavailable" } });
    }
    deleted = true;
    return route.fulfill({ status: 204, body: "" });
  });
  await page.route("**/v1/courses/*/tutor/thread", (route) =>
    route.fulfill({
      json: deleted
        ? { thread: null, messages: [] }
        : {
            thread: { id: "11111111-1111-4111-8111-111111111111" },
            messages: [
              {
                id: "m-user",
                role: "user",
                content: QUESTION,
                status: "complete",
                created_at: "2030-01-01T00:00:00Z",
              },
              {
                id: "m-ai",
                role: "assistant",
                content: REPLY,
                status: "complete",
                created_at: "2030-01-01T00:00:01Z",
              },
              {
                id: "m-cut",
                role: "assistant",
                content: "Partial",
                status: "interrupted",
                created_at: "2030-01-01T00:00:02Z",
              },
            ],
          },
    }),
  );
  return state;
}

async function seedLesson(page: Page, request: APIRequestContext) {
  await page.goto("/profile");
  const tokens = await page.evaluate(() =>
    JSON.parse(sessionStorage.getItem("duolify.native-store-fixture")!),
  );
  const seeded = await request.post(`${apiBase}/test/player/fixture`, {
    headers: { Authorization: `Bearer ${tokens.access_token}` },
  });
  expect(seeded.status()).toBe(201);
  const { lesson_id } = await seeded.json();
  await page.goto(`/lesson/${lesson_id}?returnTo=%2Fcourses`);
  await expect(page.getByText("Slide 1 of 3")).toBeVisible();
}

async function press(
  page: Page,
  mobile: boolean,
  target: ReturnType<Page["getByRole"]>,
) {
  if (mobile) await target.tap();
  else {
    await target.focus();
    await page.keyboard.press("Enter");
  }
}

const noOverflow = (page: Page) =>
  page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);

for (const colorScheme of ["light", "dark"] as const)
  for (const mobile of [false, true])
    test.describe(`tutor save and delete ${mobile ? "mobile touch" : "desktop keyboard"} ${colorScheme}`, () => {
      test.use({ hasTouch: mobile });
      test.beforeEach(async ({ page }) => {
        await page.setViewportSize(
          mobile ? { width: 390, height: 844 } : { width: 1280, height: 900 },
        );
        await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
      });

      async function openTutor(page: Page, request: APIRequestContext) {
        await seedLesson(page, request);
        await press(
          page,
          mobile,
          page.getByRole("button", { name: "Ask tutor", exact: true }),
        );
        const tutor = page
          .getByRole("region", { name: "Ask tutor" })
          .or(page.getByRole("dialog", { name: "Ask tutor" }));
        await expect(tutor).toBeVisible();
        return tutor;
      }

      test("saves a reply twice idempotently, even after a lost response, with zero credits", async ({
        page,
        request,
      }) => {
        const state = await mockThread(page, { failSaveOnce: true });
        const tutor = await openTutor(page, request);
        // Only complete assistant replies offer Save: not the question, not the interrupted prefix.
        const saves = tutor.getByRole("button", { name: "Save as quick note" });
        await expect(saves).toHaveCount(1);
        await press(page, mobile, saves);
        // The response was lost: the reply stays savable and says so.
        await expect(tutor.getByRole("alert")).toContainText(
          "Could not save the note",
        );
        await press(
          page,
          mobile,
          tutor.getByRole("button", { name: "Save as quick note" }),
        );
        const saved = tutor
          .getByRole("status")
          .filter({ hasText: "Saved to Quick notes · Enrichment pending" });
        await expect(saved).toBeVisible();
        // Zero credits did not block the raw save, and the same message was addressed both times.
        expect(state.saveRequests).toEqual(["m-ai", "m-ai"]);
        expect(state.notes.size).toBe(1);
        await expect(
          tutor.getByRole("button", { name: "Save as quick note" }),
        ).toHaveCount(0);
        await expect(
          tutor.getByRole("link", { name: "Open notes" }),
        ).toBeVisible();
        expect(await noOverflow(page)).toBe(true);
      });

      test("delete confirm names the thread, traps focus and returns it, then removes the chat", async ({
        page,
        request,
      }) => {
        const state = await mockThread(page, { failDelete: true });
        const tutor = await openTutor(page, request);
        const opener = tutor.getByRole("button", { name: "Delete chat" });
        await press(page, mobile, opener);
        const dialog = page.getByRole("dialog", { name: "Delete this chat?" });
        await expect(dialog).toBeVisible();
        await expect(dialog).toContainText("Why does the North Star");
        await expect(dialog).toContainText("3 messages");
        await expect(dialog).toContainText(
          "Quick notes you saved from it stay",
        );
        await expect(
          dialog.getByRole("button", { name: "Cancel" }),
        ).toBeFocused();
        await page.keyboard.press("Escape");
        await expect(dialog).toBeHidden();
        await expect(opener).toBeFocused();
        expect(state.deletes).toHaveLength(0);
        // A failed delete keeps the thread and offers the same action again.
        await press(page, mobile, opener);
        await press(
          page,
          mobile,
          dialog.getByRole("button", { name: "Delete", exact: true }),
        );
        await expect(dialog.getByRole("alert")).toContainText(
          "Could not delete the chat",
        );
        await expect(tutor.getByText(QUESTION)).toBeVisible();
        // Retry succeeds: messages are gone and the composer takes focus.
        state.failDelete = false;
        await press(
          page,
          mobile,
          dialog.getByRole("button", { name: "Delete", exact: true }),
        );
        await expect(dialog).toBeHidden();
        expect(state.deletes).toEqual([
          "/v1/tutor/threads/11111111-1111-4111-8111-111111111111",
          "/v1/tutor/threads/11111111-1111-4111-8111-111111111111",
        ]);
        await expect(tutor.getByText(QUESTION)).toHaveCount(0);
        await expect(tutor.getByText(REPLY)).toHaveCount(0);
        await expect(
          tutor.getByRole("textbox", { name: "Message" }),
        ).toBeFocused();
        expect(await noOverflow(page)).toBe(true);
      });

      test("offline disables save and delete with the draft notice", async ({
        page,
        context,
        request,
      }) => {
        const state = await mockThread(page);
        const tutor = await openTutor(page, request);
        await context.setOffline(true);
        await expect(
          tutor.getByText("Tutor chat needs a connection"),
        ).toBeVisible();
        await expect(
          tutor.getByRole("button", { name: "Save as quick note" }),
        ).toBeDisabled();
        await expect(
          tutor.getByRole("button", { name: "Delete chat" }),
        ).toBeDisabled();
        expect(state.saveRequests).toHaveLength(0);
        await context.setOffline(false);
        await expect(
          tutor.getByRole("button", { name: "Save as quick note" }),
        ).toBeEnabled();
      });
    });
