import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { GuidedChatDemo } from "../features/design/chat/GuidedChatDemo";
import { Sheet } from "../features/design/sheet/Sheet";
import { useState } from "react";

it("carries chat answers into the form, associates validation, and confirms only explicitly", () => {
  render(<GuidedChatDemo />);
  fireEvent.click(screen.getByRole("button", { name: "City balcony" }));
  fireEvent.click(screen.getByRole("button", { name: "Send answer" }));
  expect(screen.getByRole("textbox", { name: "Your answer" })).toHaveFocus();
  fireEvent.change(screen.getByRole("textbox", { name: "Goal" }), {
    target: { value: "Private goal" },
  });
  fireEvent.click(screen.getByRole("link", { name: "Prefer a form?" }));
  expect(screen.getByRole("textbox", { name: "Goal" })).toHaveValue(
    "Private goal",
  );
  expect(screen.getByRole("textbox", { name: "Viewing" })).toHaveValue(
    "City balcony",
  );
  const topic = screen.getByRole("textbox", { name: "Topic" });
  fireEvent.change(topic, { target: { value: "" } });
  fireEvent.click(screen.getByRole("button", { name: "Confirm brief" }));
  expect(topic).toHaveAttribute("aria-invalid", "true");
  expect(topic).toHaveAccessibleDescription("Enter a topic before confirming.");
  expect(
    screen.queryByText("Brief confirmed", { exact: true }),
  ).not.toBeInTheDocument();
  fireEvent.change(topic, { target: { value: "Astronomy" } });
  fireEvent.click(screen.getByRole("button", { name: "Confirm brief" }));
  expect(screen.getByText("Brief confirmed", { exact: true })).toBeVisible();
});

it("keeps summary and outline draft state when panel becomes sheet and returns focus", () => {
  const listeners = new Set<(event: { matches: boolean }) => void>();
  const media = {
    matches: false,
    addEventListener: (_: string, fn: (event: { matches: boolean }) => void) =>
      listeners.add(fn),
    removeEventListener: (
      _: string,
      fn: (event: { matches: boolean }) => void,
    ) => listeners.delete(fn),
  };
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => media),
  );
  const { unmount } = render(<GuidedChatDemo />);
  fireEvent.change(screen.getByRole("textbox", { name: "Goal" }), {
    target: { value: "Keep this draft" },
  });
  // Change the real responsive adapter, not a second editor instance.
  fireEvent(window, new Event("resize"));
  media.matches = true;
  act(() => listeners.forEach((listener) => listener({ matches: true })));
  fireEvent.click(screen.getByRole("button", { name: "Open course brief" }));
  const dialog = screen.getByRole("dialog", { name: "Course brief" });
  expect(within(dialog).getByRole("textbox", { name: "Goal" })).toHaveValue(
    "Keep this draft",
  );
  const goal = within(dialog).getByRole("textbox", { name: "Goal" });
  goal.focus();
  media.matches = false;
  act(() => listeners.forEach((listener) => listener({ matches: false })));
  expect(
    screen.getByRole("complementary", { name: "Course brief" }),
  ).toBeVisible();
  expect(goal).toHaveFocus();
  media.matches = true;
  act(() => listeners.forEach((listener) => listener({ matches: true })));
  fireEvent.keyDown(dialog, { key: "Escape" });
  expect(
    screen.getByRole("button", { name: "Open course brief" }),
  ).toHaveFocus();
  fireEvent.click(screen.getByRole("button", { name: "Move Planets up" }));
  fireEvent.click(screen.getByRole("button", { name: "Open outline chat" }));
  fireEvent.click(screen.getByRole("button", { name: "Apply" }));
  expect(screen.getByText("Proposal applied. Undo available.")).toBeVisible();
  fireEvent.keyDown(screen.getByRole("dialog", { name: "Outline chat" }), {
    key: "Escape",
  });
  expect(
    screen.getByRole("textbox", { name: "Title for Your first telescope" }),
  ).toBeVisible();
  unmount();
  vi.unstubAllGlobals();
});

it("traps and restores keyboard focus, pauses the host once, and allows Escape with a pending draft", () => {
  const pause = vi.fn();
  function Host() {
    const [open, setOpen] = useState(false);
    return (
      <>
        <button onClick={() => setOpen(true)}>Open sheet</button>
        <Sheet
          open={open}
          onClose={() => setOpen(false)}
          title="Draft sheet"
          sheetId="fixture_sheet"
          onPausePlayer={pause}
        >
          <input aria-label="Draft" />
          <button>Last</button>
        </Sheet>
      </>
    );
  }
  render(<Host />);
  const trigger = screen.getByRole("button", { name: "Open sheet" });
  trigger.focus();
  fireEvent.click(trigger);
  const dialog = screen.getByRole("dialog");
  expect(pause).toHaveBeenCalledTimes(1);
  const close = within(dialog).getByRole("button", {
    name: "Close Draft sheet",
  });
  expect(close).toHaveFocus();
  const last = within(dialog).getByRole("button", { name: "Last" });
  last.focus();
  fireEvent.keyDown(dialog, { key: "Tab" });
  expect(close).toHaveFocus();
  fireEvent.keyDown(dialog, { key: "Tab", shiftKey: true });
  expect(last).toHaveFocus();
  fireEvent.keyDown(dialog, { key: "Escape" });
  expect(trigger).toHaveFocus();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
