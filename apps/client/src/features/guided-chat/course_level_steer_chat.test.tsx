import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { GuidedCourseLevel } from "./GuidedCourseLevel";
import { InstructionPanel } from "../steering/InstructionPanel";
import type { InstructionService } from "../steering/api";

const limits = {
  max_characters: 500,
  max_active: 20,
  max_total_characters: 4000,
  adds_per_hour: 10,
};
const service = (add = vi.fn().mockResolvedValue({ id: "i1", text: "x" })) =>
  ({
    latest: vi.fn(),
    list: vi.fn().mockResolvedValue({
      instructions: [],
      limits,
      active_count: 0,
      active_characters: 0,
    }),
    add,
    edit: vi.fn(),
    remove: vi.fn(),
  }) satisfies InstructionService;

describe("course.level as a chat", () => {
  it("starts on the current level, taps another and saves on confirm", async () => {
    const save = vi.fn().mockResolvedValue({ level_override: "advanced" });
    render(
      <GuidedCourseLevel
        courseId="c1"
        courseTitle="Spanish"
        levelOverride=""
        globalLevel="beginner"
        onSaveLevel={save}
      />,
    );
    expect(screen.getByText("Use this level?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Change answer/ }));
    fireEvent.click(screen.getByRole("radio", { name: "Advanced" }), {
      detail: 1,
    });
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(save).toHaveBeenCalledWith("c1", "advanced"));
    expect(await screen.findByText("Level saved.")).toBeTruthy();
  });
});

describe("course.steer as a chat", () => {
  it("collects scope and text, then sends the instruction once", async () => {
    const svc = service();
    render(<InstructionPanel courseId="c1" service={svc} guided />);
    await screen.findByText("Apply to which lessons?");
    fireEvent.click(screen.getByRole("radio", { name: "Rest of course" }), {
      detail: 1,
    });
    fireEvent.change(
      screen.getByRole("textbox", { name: /What should change/ }),
      {
        target: { value: "More worked examples" },
      },
    );
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    fireEvent.click(await screen.findByRole("button", { name: "Confirm" }));
    await waitFor(() =>
      expect(svc.add).toHaveBeenCalledWith("c1", {
        text: "More worked examples",
        scope: "rest_of_course",
      }),
    );
    expect(svc.add).toHaveBeenCalledTimes(1);
  });
});
