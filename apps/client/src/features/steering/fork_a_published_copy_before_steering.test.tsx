import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { InstructionPanel } from "./InstructionPanel";
import type { InstructionList, InstructionService } from "./api";

const limits = {
  max_characters: 500,
  max_active: 20,
  max_total_characters: 4000,
  adds_per_hour: 10,
};
const copyList = (forked: boolean): InstructionList => ({
  instructions: [],
  limits,
  active_count: 0,
  active_characters: 0,
  fork: { required: !forked, forked, kept_lessons: 4, lesson_credits: 190 },
});
const saved = {
  id: "i1",
  text: "More examples in Go",
  scope: "next_lesson",
  status: "active",
};
const service = (
  list: InstructionList,
  add = vi.fn().mockResolvedValue(saved),
) =>
  ({
    latest: vi.fn(),
    edit: vi.fn(),
    remove: vi.fn(),
    list: vi.fn().mockResolvedValue(list),
    add,
  }) satisfies InstructionService;

async function fillAndSave() {
  await screen.findByText("No instruction yet.");
  fireEvent.change(screen.getByLabelText("New instruction"), {
    target: { value: "More examples in Go" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save instruction" }));
}

describe("fork a published copy before steering", () => {
  it("declining the warning sends nothing and keeps the draft", async () => {
    const s = service(copyList(false));
    render(<InstructionPanel courseId="c1" service={s} />);
    await fillAndSave();
    const dialog = await screen.findByRole("dialog", {
      name: "Make this course your own?",
    });
    expect(dialog).toHaveTextContent("Lessons 1–4 stay as they are");
    expect(dialog).toHaveTextContent("about 190 credits each");
    expect(dialog).toHaveTextContent(/won.t receive/i);
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(s.add).not.toHaveBeenCalled();
    expect(screen.getByLabelText("New instruction")).toHaveValue(
      "More examples in Go",
    );
    expect(
      screen.getByRole("button", { name: "Save instruction" }),
    ).toHaveFocus();
  });

  it("falls back to price-free wording when no active price is known", async () => {
    const list = copyList(false);
    list.fork = { required: true, forked: false, kept_lessons: 4 };
    render(<InstructionPanel courseId="c1" service={service(list)} />);
    await fillAndSave();
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent(
      /cost credits at the current lesson price/i,
    );
    expect(dialog).not.toHaveTextContent(/about \d+ credits/);
  });

  it("Escape declines too", async () => {
    const s = service(copyList(false));
    render(<InstructionPanel courseId="c1" service={s} />);
    await fillAndSave();
    const dialog = await screen.findByRole("dialog");
    fireEvent.keyDown(dialog, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(s.add).not.toHaveBeenCalled();
  });

  it("accepting saves with the confirmation and marks the copy forked", async () => {
    const s = service(copyList(false));
    render(<InstructionPanel courseId="c1" service={s} />);
    await fillAndSave();
    fireEvent.click(
      await screen.findByRole("button", { name: "Make it mine" }),
    );
    await screen.findByText("Active");
    expect(s.add).toHaveBeenCalledWith("c1", {
      text: "More examples in Go",
      scope: "next_lesson",
      confirm_fork: true,
    });
    expect(screen.queryByRole("dialog")).toBeNull();
    // A second instruction needs no new warning.
    fireEvent.change(screen.getByLabelText("New instruction"), {
      target: { value: "Shorter lessons" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save instruction" }));
    await waitFor(() => expect(s.add).toHaveBeenCalledTimes(2));
    expect(s.add).toHaveBeenLastCalledWith("c1", {
      text: "Shorter lessons",
      scope: "next_lesson",
    });
  });

  it("an already forked or private course never shows the warning", async () => {
    const s = service(copyList(true));
    render(<InstructionPanel courseId="c1" service={s} />);
    await fillAndSave();
    await screen.findByText("Active");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(s.add).toHaveBeenCalledWith("c1", {
      text: "More examples in Go",
      scope: "next_lesson",
    });
  });

  it("opens the warning when the server asks for confirmation", async () => {
    const add = vi
      .fn()
      .mockRejectedValueOnce(new Error("fork_confirmation_required"))
      .mockResolvedValue(saved);
    const s = service({ ...copyList(true), fork: undefined }, add);
    render(<InstructionPanel courseId="c1" service={s} />);
    await fillAndSave();
    fireEvent.click(
      await screen.findByRole("button", { name: "Make it mine" }),
    );
    await screen.findByText("Active");
    expect(add).toHaveBeenLastCalledWith("c1", {
      text: "More examples in Go",
      scope: "next_lesson",
      confirm_fork: true,
    });
  });
});
