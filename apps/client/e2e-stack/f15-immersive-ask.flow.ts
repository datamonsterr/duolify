import { learnerWithCourse, requireLessonGeneration } from "./lesson";
import { expect, test } from "./stack";

test.use({ flowId: "F15" });

test(
  "F15 immersive lesson: select a region, ask by text, save the reply as a Quick note",
  {
    annotation: {
      type: "flow",
      description:
        "F15 Immersive lesson: select a region, ask by text, save as Quick note",
    },
  },
  async ({ page, request, flow }) => {
    await requireLessonGeneration(request);
    await learnerWithCourse(page, "f15");

    // Prepare and open the first lesson (quoted, paid from daily free credits).
    await page
      .getByRole("button", { name: /Prepare lesson/i })
      .first()
      .click();
    await page.getByRole("button", { name: "Accept quote terms" }).click();
    const start = page.getByRole("link", { name: /Start lesson/i }).first();
    await expect(start).toBeVisible({ timeout: 180_000 });
    await start.click();
    await expect(page.getByText(/Slide 1 of \d+/)).toBeVisible();
    await flow.step("lesson player, framed");

    // Enter immersive mode: full screen stage with the controls layer.
    await page.getByTestId("immersive-enter").click();
    await expect(page.locator(".player.immersive")).toBeVisible();
    await expect(page.getByTestId("immersive-layer")).toBeVisible();
    await flow.step("immersive mode");

    // Select an area from the side rail (the keyboard-reachable path).
    const handle = page.getByTestId("immersive-side-rail-handle");
    if ((await handle.getAttribute("aria-expanded")) !== "true")
      await handle.click();
    await page.getByTestId("immersive-select-area").click();
    await expect(page.getByTestId("region-box")).toBeFocused();
    await flow.step("area selected");

    // Ask about it by text: the opaque answer panel opens on the right.
    await page.getByTestId("region-ask").click();
    const panel = page.getByTestId("answer-panel");
    await expect(panel).toBeVisible();
    await expect(page.getByTestId("region-outline")).toBeVisible();
    await panel
      .getByRole("textbox", { name: "Your question" })
      .fill("What is this part of the slide about, in one sentence?");
    await flow.step("question typed in the answer panel");
    await panel.getByTestId("answer-send").click();
    const reply = panel.locator(".imm-answer-assistant");
    await expect(reply).toHaveCount(1);
    await expect(reply.first()).not.toBeEmpty();
    await expect(page.getByTestId("answer-save-note")).toBeVisible();
    const words = ((await reply.first().innerText()) || "")
      .replace(/Save as Quick note|Saved as Quick note/g, "")
      .trim()
      .split(/\s+/)
      .slice(0, 4)
      .join(" ");
    await expect(panel.getByRole("alert")).toHaveCount(0);
    await flow.step("reply in the panel");

    // Save the reply as a Quick note anchored to the area.
    await panel.getByTestId("answer-save-note").click();
    await expect(page.getByTestId("toast")).toContainText(
      "Saved as Quick note",
    );
    await flow.step("reply saved as a Quick note");

    await page.getByTestId("answer-close").dispatchEvent("click");
    await expect(page.getByTestId("answer-panel")).toHaveCount(0);
    await flow.step("panel closed, playback resumed");

    // The note is in the Learner's notes.
    await page.goto("/notes");
    await expect(
      page.getByRole("heading", { name: "Quick notes" }),
    ).toBeVisible();
    await expect(
      page
        .getByRole("button", {
          name: new RegExp(words.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")),
        })
        .first(),
    ).toBeVisible();
    await flow.step("note in the notes list");
  },
);
