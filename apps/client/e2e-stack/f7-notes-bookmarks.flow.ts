import { readFileSync } from "node:fs";
import {
  learnerWithCourse,
  playFirstLesson,
  requireLessonGeneration,
} from "./lesson";
import { expect, test } from "./stack";

test.use({ flowId: "F7" });

test(
  "F7 quick note: save, list, export; bookmark a slide",
  {
    annotation: {
      type: "flow",
      description: "F7 Quick note save, list, export; bookmark a slide",
    },
  },
  async ({ page, request, flow }) => {
    await requireLessonGeneration(request);
    await learnerWithCourse(page, "f7");
    await playFirstLesson(page, flow);

    await page.getByRole("button", { name: "Bookmark", exact: true }).click();
    await expect(
      page.getByRole("status").filter({ hasText: "Slide bookmarked" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: /^(Bookmark|Saved)$/ }),
    ).toHaveAttribute("aria-pressed", "true");
    await flow.step("slide bookmarked");

    const note = `Stars fuse hydrogen ${Date.now().toString(36)}`;
    await page.getByRole("button", { name: "Quick note", exact: true }).click();
    const panel = page.getByRole("region", { name: "Quick note" });
    await panel.getByLabel("Your note").fill(note);
    // Raw note only: enrichment is a separate, credit-using action.
    const enrich = panel.getByRole("checkbox", { name: /Enrich with AI/ });
    if ((await enrich.count()) && (await enrich.isChecked()))
      await enrich.uncheck();
    await flow.step("quick note typed");
    await panel.getByRole("button", { name: "Save quick note" }).click();
    await expect(
      panel.getByRole("status").filter({ hasText: /Saved/ }),
    ).toBeVisible();
    await flow.step("quick note saved");

    await panel.getByRole("link", { name: "Open notes" }).click();
    await expect(page).toHaveURL(/\/notes/);
    await expect(
      page.getByRole("heading", { name: "Quick notes" }),
    ).toBeVisible();
    const item = page.getByRole("button", { name: new RegExp(`: ${note}$`) });
    await expect(item).toBeVisible();
    await flow.step("notes list");
    await item.click();
    const download = page.waitForEvent("download");
    await page.getByRole("button", { name: "Export Markdown" }).click();
    const file = await download;
    expect(file.suggestedFilename()).toMatch(/\.md$/);
    expect(readFileSync(await file.path(), "utf8")).toContain(note);
    await flow.step("note exported as markdown");

    await page.goto("/bookmarks");
    await expect(
      page.getByRole("heading", { name: "Bookmarks", level: 1 }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: /^Open .+, slide \d+$/ }),
    ).toHaveCount(1);
    await flow.step("bookmarks list");
  },
);
