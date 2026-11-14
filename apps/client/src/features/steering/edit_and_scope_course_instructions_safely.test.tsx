import {
  render,
  screen,
  fireEvent,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { InstructionPanel } from "./InstructionPanel";
import type { Instruction, InstructionService } from "./api";

const limits = {
  max_characters: 500,
  max_active: 20,
  max_total_characters: 4000,
  adds_per_hour: 10,
};
const rec = (o: Partial<Instruction> & { id: string; text: string }) =>
  ({
    scope: "next_lesson",
    status: "active",
    version: 1,
    added_at: "2026-11-07T10:00:00Z",
    applied_lesson_id: null,
    delivery: "",
    ...o,
  }) as Instruction;
const listOf = (instructions: Instruction[], l = limits) => {
  const active = instructions.filter((i) => i.status === "active");
  return {
    instructions,
    limits: l,
    active_count: active.length,
    active_characters: active.reduce(
      (n, i) => n + Array.from(i.text).length,
      0,
    ),
  };
};
const fake = (instructions: Instruction[], l = limits) =>
  ({
    latest: vi.fn(),
    list: vi.fn().mockResolvedValue(listOf(instructions, l)),
    add: vi.fn(),
    edit: vi.fn(),
    remove: vi.fn().mockResolvedValue(undefined),
  }) satisfies InstructionService;
const row = (text: string) =>
  screen.getAllByRole("listitem").find((n) => n.textContent?.includes(text))!;
const mobile = () =>
  vi.stubGlobal("matchMedia", (q: string) => ({
    matches: true,
    media: q,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
afterEach(() => vi.unstubAllGlobals());

describe("edit and scope course instructions safely", () => {
  it("shows API limits, scope, status and delivery hints per instruction", async () => {
    const service = fake(
      [
        rec({ id: "a", text: "Running one", delivery: "running" }),
        rec({ id: "b", text: "Waiting one", delivery: "waits" }),
        rec({ id: "c", text: "Ready one", delivery: "ready" }),
        rec({
          id: "d",
          text: "Rest one",
          scope: "rest_of_course",
        }),
        rec({
          id: "e",
          text: "Old one",
          status: "applied",
          applied_lesson_id: "lesson-6",
        }),
      ],
      { ...limits, max_total_characters: 3000 },
    );
    render(<InstructionPanel courseId="c1" service={service} />);
    await screen.findByText("4 active · 39 / 3,000 characters");
    expect(
      within(row("Running one")).getByText(
        "Used by the lesson being prepared; applied when it is delivered",
      ),
    ).toBeVisible();
    expect(
      within(row("Waiting one")).getByText(
        "Waits for the lesson after the one being prepared",
      ),
    ).toBeVisible();
    expect(
      within(row("Ready one")).getByText(
        "Applies to the next lesson to be prepared",
      ),
    ).toBeVisible();
    expect(within(row("Rest one")).getByText("Rest of course")).toBeVisible();
    expect(within(row("Rest one")).queryByText(/prepared/)).toBeNull();
    expect(
      within(row("Old one")).getByText("Applied to lesson lesson-6"),
    ).toBeVisible();
    expect(
      within(row("Old one")).queryByRole("button", { name: "Edit" }),
    ).toBeNull();
    expect(screen.getByText(/newest.*wins/i)).toBeVisible();
    expect(row("Running one").querySelector("[dir=auto]")).not.toBeNull();
  });

  it("counts characters by code point at 500/501 when editing", async () => {
    const service = fake([rec({ id: "a", text: "Go" })]);
    render(<InstructionPanel courseId="c1" service={service} />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const box = screen.getByLabelText("Edit instruction");
    expect(box).toHaveValue("Go");
    fireEvent.change(box, { target: { value: "😀".repeat(500) } });
    expect(screen.getByText(/500 \/ 500/)).toBeVisible();
    expect(screen.getByRole("button", { name: "Save changes" })).toBeEnabled();
    fireEvent.change(box, { target: { value: "語".repeat(501) } });
    expect(screen.getByText(/501 \/ 500/)).toBeVisible();
    expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled();
    fireEvent.change(box, { target: { value: "  " } });
    expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled();
  });

  it("sends expected_version, keeps the draft on screening_unavailable and retries", async () => {
    const service = fake([rec({ id: "a", text: "Go", version: 3 })]);
    service.edit
      .mockRejectedValueOnce(new Error("screening_unavailable"))
      .mockResolvedValue(rec({ id: "a", text: "Rust", version: 4 }));
    render(<InstructionPanel courseId="c1" service={service} />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByLabelText("Edit instruction"), {
      target: { value: "Rust" },
    });
    fireEvent.click(
      within(screen.getByRole("group", { name: "Change scope" })).getByRole(
        "radio",
        { name: "Rest of course" },
      ),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await screen.findByText("screening_unavailable");
    expect(screen.getByLabelText("Edit instruction")).toHaveValue("Rust");
    fireEvent.click(screen.getByRole("button", { name: "Retry saving" }));
    await waitFor(() => expect(service.edit).toHaveBeenCalledTimes(2));
    expect(service.edit).toHaveBeenLastCalledWith("c1", "a", {
      expected_version: 3,
      text: "Rust",
      scope: "rest_of_course",
    });
    await waitFor(() =>
      expect(screen.queryByLabelText("Edit instruction")).toBeNull(),
    );
    expect(screen.getByRole("status")).toHaveTextContent(/saved/i);
    expect(screen.getByRole("button", { name: "Edit" })).toHaveFocus();
  });

  it("shows re-screening reason codes and keeps the draft", async () => {
    const service = fake([rec({ id: "a", text: "Go" })]);
    service.edit.mockRejectedValue(new Error("urls_not_allowed"));
    render(<InstructionPanel courseId="c1" service={service} />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByLabelText("Edit instruction"), {
      target: { value: "see http://x.y" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "urls_not_allowed",
    );
    expect(screen.getByLabelText("Edit instruction")).toHaveValue(
      "see http://x.y",
    );
  });

  it("reloads the list on version_conflict, keeps the draft and uses the new version", async () => {
    const service = fake([rec({ id: "a", text: "Go", version: 1 })]);
    service.edit
      .mockRejectedValueOnce(new Error("version_conflict"))
      .mockResolvedValue(rec({ id: "a", text: "Rust", version: 6 }));
    render(<InstructionPanel courseId="c1" service={service} />);
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByLabelText("Edit instruction"), {
      target: { value: "Rust" },
    });
    service.list.mockResolvedValue(
      listOf([rec({ id: "a", text: "Go elsewhere", version: 5 })]),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await screen.findByText(/version_conflict/);
    await waitFor(() => expect(service.list).toHaveBeenCalledTimes(2));
    expect(screen.getByLabelText("Edit instruction")).toHaveValue("Rust");
    fireEvent.click(screen.getByRole("button", { name: "Retry saving" }));
    await waitFor(() => expect(service.edit).toHaveBeenCalledTimes(2));
    expect(service.edit).toHaveBeenLastCalledWith("c1", "a", {
      expected_version: 5,
      text: "Rust",
      scope: "next_lesson",
    });
  });

  it("asks before deleting, explains past lessons, and Escape cancels with focus back", async () => {
    const service = fake([
      rec({ id: "a", text: "Go" }),
      rec({ id: "b", text: "Java", scope: "rest_of_course" }),
    ]);
    render(<InstructionPanel courseId="c1" service={service} />);
    await screen.findByText("2 active · 6 / 4,000 characters");
    const del = within(row("Go")).getByRole("button", { name: "Delete" });
    fireEvent.click(del);
    expect(
      screen.getByText(/Past lessons never change/, {
        selector: "p.steering-confirm",
      }),
    ).toBeVisible();
    expect(service.remove).not.toHaveBeenCalled();
    fireEvent.keyDown(screen.getByRole("button", { name: "Keep" }), {
      key: "Escape",
    });
    expect(
      screen.queryByRole("button", { name: "Delete instruction" }),
    ).toBeNull();
    expect(
      within(row("Go")).getByRole("button", { name: "Delete" }),
    ).toHaveFocus();
    fireEvent.click(within(row("Go")).getByRole("button", { name: "Delete" }));
    service.list.mockResolvedValue(
      listOf([rec({ id: "b", text: "Java", scope: "rest_of_course" })]),
    );
    fireEvent.click(screen.getByRole("button", { name: "Delete instruction" }));
    await screen.findByText("1 active · 4 / 4,000 characters");
    expect(service.remove).toHaveBeenCalledWith("c1", "a");
    expect(screen.getByRole("status")).toHaveTextContent(/deleted/i);
  });

  it("recovers from a failed delete with a retry and loads offline errors", async () => {
    const service = fake([rec({ id: "a", text: "Go" })]);
    service.list.mockRejectedValueOnce(new Error("offline"));
    render(<InstructionPanel courseId="c1" service={service} />);
    await screen.findByRole("alert");
    fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    service.list.mockResolvedValue(listOf([rec({ id: "a", text: "Go" })]));
    await screen.findByText("Go");
    service.remove.mockRejectedValueOnce(new Error("not_found"));
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete instruction" }));
    await screen.findByText(/not_found/);
    expect(screen.getByText("Go")).toBeVisible();
  });

  it("shows the empty state with zero counts", async () => {
    render(<InstructionPanel courseId="c1" service={fake([])} />);
    await screen.findByText("No instruction yet.");
    expect(screen.getByText("0 active · 0 / 4,000 characters")).toBeVisible();
  });

  it("splits into list and add/edit screens on mobile with Back and focus return", async () => {
    mobile();
    const service = fake([rec({ id: "a", text: "Go" })]);
    service.add.mockResolvedValue(rec({ id: "b", text: "New" }));
    render(<InstructionPanel courseId="c1" service={service} />);
    const add = await screen.findByRole("button", { name: "Add instruction" });
    expect(screen.queryByLabelText("New instruction")).toBeNull();
    fireEvent.click(add);
    expect(screen.getByLabelText("New instruction")).toBeVisible();
    expect(screen.queryByRole("listitem")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(
      await screen.findByRole("button", { name: "Add instruction" }),
    ).toHaveFocus();
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    expect(screen.getByLabelText("Edit instruction")).toBeVisible();
    expect(screen.queryByRole("listitem")).toBeNull();
    fireEvent.keyDown(screen.getByLabelText("Edit instruction"), {
      key: "Escape",
    });
    expect(await screen.findByRole("button", { name: "Edit" })).toHaveFocus();
    fireEvent.click(screen.getByRole("button", { name: "Add instruction" }));
    fireEvent.change(screen.getByLabelText("New instruction"), {
      target: { value: "New" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save instruction" }));
    await waitFor(() => expect(service.add).toHaveBeenCalled());
    expect(
      await screen.findByRole("button", { name: "Add instruction" }),
    ).toBeVisible();
  });
});
