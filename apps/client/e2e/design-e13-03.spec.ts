import { expect, test } from "@playwright/test";

for (const width of [1280, 320]) {
  for (const colorScheme of ["light", "dark"] as const) {
    test(`guided chat, form, outline and sheet parity at ${width}px ${colorScheme}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
      await page.goto("/guided-chat.html");
      await expect(
        page.getByRole("heading", { name: "Let's shape your course" }),
      ).toBeVisible();
      await page.screenshot({
        path: `/tmp/e13-03-${width}-${colorScheme}.png`,
      });
      await page
        .getByRole("button", { name: "City balcony", exact: true })
        .click();
      await expect(
        page.getByRole("button", { name: "City balcony", exact: true }),
      ).toHaveAttribute("aria-pressed", "true");
      await page.getByRole("button", { name: "Send answer" }).click();
      await expect(
        page.getByRole("textbox", { name: "Your answer" }),
      ).toBeFocused();
      if (width < 768)
        await page.getByRole("button", { name: "Open course brief" }).click();
      await page
        .getByRole("textbox", { name: "Goal", exact: true })
        .fill("Explain the Moon to my kids");
      await expect(
        page.getByText("Unsaved changes", { exact: true }).first(),
      ).toBeVisible();
      await page
        .getByRole("button", { name: "Confirm brief", exact: true })
        .click();
      await expect(
        page.getByText("Brief confirmed", { exact: true }),
      ).toBeVisible();
      if (width < 768) {
        await page.keyboard.press("Escape");
        await expect(
          page.getByRole("button", { name: "Open course brief" }),
        ).toBeFocused();
      }
      await page.getByRole("link", { name: "Prefer a form?" }).click();
      await expect(
        page.getByRole("textbox", { name: "Goal", exact: true }),
      ).toHaveValue("Explain the Moon to my kids");
      await page.getByRole("radio", { name: "Beginner", exact: true }).check();
      await page
        .getByRole("checkbox", { name: "Send learning reminders" })
        .check();
      await page.getByRole("link", { name: "Back to chat" }).click();
      const move = page.getByRole("button", {
        name: "Move Planets up",
        exact: true,
      });
      await move.focus();
      await page.keyboard.press("Enter");
      await expect(move).toBeFocused();
      await expect(
        page
          .getByRole("status")
          .filter({ hasText: "Planets moved to position 2" }),
      ).toBeVisible();
      if (width < 768)
        await page.getByRole("button", { name: "Open outline chat" }).click();
      for (const label of ["Added", "Removed", "Moved", "Retitled"])
        await expect(page.getByText(label, { exact: true })).toBeVisible();
      await page.getByRole("button", { name: "Discard", exact: true }).click();
      await expect(
        page.getByText("Proposal discarded. Outline unchanged."),
      ).toBeVisible();
      await page.getByRole("button", { name: "Show suggestion" }).click();
      await page.getByRole("button", { name: "Apply", exact: true }).click();
      await expect(
        page.getByText("Proposal applied. Undo available."),
      ).toBeVisible();
      if (width < 768) await page.keyboard.press("Escape");
      await expect(
        page.getByRole("textbox", { name: "Title for Your first telescope" }),
      ).toBeVisible();
      await page.getByRole("button", { name: "Undo", exact: true }).click();
      await expect(
        page.getByRole("textbox", { name: "Title for Your first telescope" }),
      ).toHaveCount(0);
      await page.getByRole("button", { name: "Open keyboard sheet" }).click();
      const dialog = page.getByRole("dialog", { name: "Keyboard sheet" });
      await expect(dialog).toBeVisible();
      await expect(page.getByText("Player paused")).toBeVisible();
      await dialog.getByRole("button", { name: "Last action" }).focus();
      await page.keyboard.press("Tab");
      await expect(
        dialog.getByRole("button", { name: "Close Keyboard sheet" }),
      ).toBeFocused();
      await page.keyboard.press("Shift+Tab");
      await expect(
        dialog.getByRole("button", { name: "Last action" }),
      ).toBeFocused();
      await page.keyboard.press("Escape");
      await expect(
        page.getByRole("button", { name: "Open keyboard sheet" }),
      ).toBeFocused();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    });
  }
  test(`recovery and long content at ${width}px`, async ({ page, context }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/guided-chat.html");
    await page
      .getByRole("combobox", { name: "Fixture state" })
      .selectOption("loading");
    await expect(page.getByText("Duolify is typing…")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Send answer" }),
    ).toBeDisabled();
    await page
      .getByRole("combobox", { name: "Fixture state" })
      .selectOption("error");
    await expect(page.getByRole("alert")).toContainText("Chat is unavailable");
    await page.getByRole("button", { name: "Retry chat" }).click();
    await expect(page.getByRole("alert")).toHaveCount(0);
    await page
      .getByRole("combobox", { name: "Fixture state" })
      .selectOption("empty");
    await expect(
      page.getByText("No messages yet. You can start with an answer."),
    ).toBeVisible();
    await context.setOffline(true);
    await expect(
      page.getByText(
        "Offline. Your draft stays here; use the form or retry when connected.",
      ),
    ).toBeVisible();
    await context.setOffline(false);
    if (width < 768)
      await page.getByRole("button", { name: "Open course brief" }).click();
    await page
      .getByRole("textbox", { name: "Goal", exact: true })
      .fill("Ephemeral private draft");
    if (width < 768) await page.keyboard.press("Escape");
    await page
      .getByRole("combobox", { name: "Fixture state" })
      .selectOption("long");
    await page.evaluate(() => {
      document.documentElement.style.fontSize = "200%";
      document.documentElement.dir = "rtl";
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.reload();
    if (width < 768)
      await page.getByRole("button", { name: "Open course brief" }).click();
    await expect(
      page.getByRole("textbox", { name: "Goal", exact: true }),
    ).toHaveValue("Explain the night sky to my kids");
  });
}

test("moving down retains focus and resizing keeps one editor draft", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/guided-chat.html");
  const moveUnit = page.getByRole("button", {
    name: "Move Finding your bearings down",
    exact: true,
  });
  await moveUnit.focus();
  await page.keyboard.press("Enter");
  await expect(moveUnit).toBeFocused();
  const moveLesson = page.getByRole("button", {
    name: "Move A map of the night down",
    exact: true,
  });
  await moveLesson.focus();
  await page.keyboard.press("Enter");
  await expect(moveLesson).toBeFocused();
  await page
    .getByRole("textbox", { name: "Goal", exact: true })
    .fill("Draft during resize");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "Open course brief" }).click();
  await expect(
    page.getByRole("textbox", { name: "Goal", exact: true }),
  ).toHaveValue("Draft during resize");
  await page
    .getByRole("textbox", { name: "Goal", exact: true })
    .fill("Edited inside the sheet");
  await page.setViewportSize({ width: 1280, height: 900 });
  await expect(
    page.getByRole("complementary", { name: "Course brief" }),
  ).toBeVisible();
  await expect(
    page.getByRole("textbox", { name: "Goal", exact: true }),
  ).toHaveValue("Edited inside the sheet");
});

test.describe("touch and confirmation recovery", () => {
  test.use({ hasTouch: true });
  test("mobile supports multi-select, pending, validation and error recovery", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/guided-chat.html");
    for (const label of ["City balcony", "Both"])
      await page.getByRole("button", { name: label, exact: true }).tap();
    await page.getByRole("button", { name: "Send answer" }).tap();
    await page.getByRole("button", { name: "Open course brief" }).tap();
    await expect(
      page.getByRole("textbox", { name: "Viewing", exact: true }),
    ).toHaveValue("City balcony · Both");
    for (const target of await page
      .getByRole("dialog")
      .locator("button,input")
      .all()) {
      const box = await target.boundingBox();
      expect(box!.height).toBeGreaterThanOrEqual(44);
      expect(box!.width).toBeGreaterThanOrEqual(44);
    }
    await page.keyboard.press("Escape");
    await page
      .getByRole("combobox", { name: "Fixture state" })
      .selectOption("pending");
    await page.getByRole("button", { name: "Open course brief" }).tap();
    await expect(
      page.getByRole("button", { name: "Preparing…" }),
    ).toBeDisabled();
    await expect(
      page.getByRole("button", { name: "Preparing…" }),
    ).toHaveAttribute("aria-busy", "true");
    await page.keyboard.press("Escape");
    await page
      .getByRole("combobox", { name: "Fixture state" })
      .selectOption("confirm-error");
    await page.getByRole("button", { name: "Open course brief" }).tap();
    await expect(page.getByRole("alert")).toContainText("Your draft is kept");
    await expect(
      page.getByRole("textbox", { name: "Viewing", exact: true }),
    ).toHaveValue("City balcony · Both");
    await page.keyboard.press("Escape");
    await page
      .getByRole("combobox", { name: "Fixture state" })
      .selectOption("ready");
    await page.getByRole("button", { name: "Open course brief" }).tap();
    await page
      .getByRole("button", { name: "Confirm brief", exact: true })
      .tap();
    await expect(
      page.getByText("Brief confirmed", { exact: true }),
    ).toBeVisible();
  });
});
