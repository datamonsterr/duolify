import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { GuidedCourseLevel } from "../guided-chat/GuidedCourseLevel";
import { actionFooterViolations } from "./humanFactors";
import { ActionFooter } from "./sheet/ActionFooter";
import { Sheet } from "./sheet/Sheet";
import { Button } from "./Button";

const src = join(__dirname, "..", "..");
const files = (dir: string): string[] =>
  readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory()
      ? files(path)
      : /\.tsx$/.test(name) && !/\.test\./.test(name)
        ? [path]
        : [];
  });
// Dialog hosts whose action buttons live in a child that already uses ActionFooter,
// or that have no action row at all.
const hosts = [
  "LearnPath.tsx",
  "steering/SteerCourse.tsx",
  "shell/AdaptiveLayouts.tsx",
  "hotkeys/ShortcutHelp.tsx",
  "onboarding/Modal.tsx",
  "design/sheet/ResponsivePanel.tsx",
  "design/sheet/ModalDialog.tsx",
  "sharing/ShareCourse.tsx",
  "design/chat/GuidedChatDemo.tsx",
];

describe("dialog action convention (bottom, right-aligned, primary right-most)", () => {
  it("every dialog, sheet and modal component uses ActionFooter", () => {
    const missing = files(src)
      .filter((f) =>
        /<dialog|role="dialog"|<Sheet\b/.test(readFileSync(f, "utf8")),
      )
      .filter((f) => !readFileSync(f, "utf8").includes("ActionFooter"))
      .map((f) => f.slice(src.length + 1))
      .filter((f) => !hosts.some((h) => f.endsWith(h)));
    expect(missing).toEqual([]);
  });

  it("the footer CSS right-aligns, and fills the width on phones", () => {
    const css = readFileSync(
      join(__dirname, "sheet", "action-footer.css"),
      "utf8",
    );
    expect(css).toMatch(/justify-content:\s*flex-end/);
    expect(css).toMatch(/max-width:\s*37\.4375rem[\s\S]*inline-size:\s*100%/);
  });

  it("flags content after the footer and a primary that is not last", () => {
    const { container } = render(
      <div>
        <ActionFooter>
          <Button>Save</Button>
          <Button variant="secondary">Cancel</Button>
        </ActionFooter>
        <p>after</p>
      </div>,
    );
    expect(actionFooterViolations(container)).toHaveLength(2);
  });

  it("Sheet puts its footer last, cancel before primary", () => {
    render(
      <Sheet
        open
        onClose={() => undefined}
        title="Demo"
        sheetId="course_steering"
        footer={
          <>
            <Button variant="secondary">Cancel</Button>
            <Button>Save</Button>
          </>
        }
      >
        <p>body</p>
      </Sheet>,
    );
    const dialog = screen.getByRole("dialog", { hidden: true });
    expect(dialog.lastElementChild?.hasAttribute("data-action-footer")).toBe(
      true,
    );
    expect(actionFooterViolations(dialog)).toEqual([]);
  });

  it("a guided chat renders its actions in the footer, after the chat body", () => {
    const { container } = render(
      <GuidedCourseLevel
        courseId="c1"
        courseTitle="Spanish"
        levelOverride=""
        globalLevel="beginner"
        onSaveLevel={vi.fn()}
      />,
    );
    const confirm = screen.getByRole("button", { name: "Confirm" });
    expect(confirm.closest("[data-action-footer]")).not.toBeNull();
    expect(actionFooterViolations(container)).toEqual([]);
  });
});
