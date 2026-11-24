import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { UiPreferencesProvider } from "../design/localization/Preferences";
import { SidebarTools } from "./SidebarTools";

function setup(onShowHelp = vi.fn()) {
  render(
    <UiPreferencesProvider>
      <SidebarTools surface="courses" onShowHelp={onShowHelp} />
    </UiPreferencesProvider>,
  );
  return onShowHelp;
}

it("has three icon-only buttons with names and no visible text", () => {
  setup();
  const buttons = screen.getAllByRole("button");
  expect(buttons).toHaveLength(3);
  for (const b of buttons) expect(b.textContent).toBe("");
  expect(screen.getByRole("button", { name: "Language" })).toBeVisible();
  expect(screen.getByRole("button", { name: "Dark theme" })).toBeVisible();
  expect(screen.getByRole("button", { name: "Shortcuts" })).toBeVisible();
});

it("toggles the theme", () => {
  setup();
  const theme = screen.getByRole("button", { name: "Dark theme" });
  fireEvent.click(theme);
  expect(theme).toHaveAttribute("aria-pressed", "true");
  expect(document.documentElement.dataset.theme).toBe("dark");
  fireEvent.click(theme);
  expect(theme).toHaveAttribute("aria-pressed", "false");
});

it("switches language from the menu, Escape closes it and focus returns", () => {
  setup();
  const trigger = screen.getByRole("button", { name: "Language" });
  fireEvent.click(trigger);
  expect(
    screen.getByRole("menuitemradio", { name: "English" }),
  ).toHaveAttribute("aria-checked", "true");
  fireEvent.keyDown(document.activeElement!, { key: "Escape" });
  expect(screen.queryByRole("menu")).toBeNull();
  expect(trigger).toHaveFocus();
  fireEvent.click(trigger);
  fireEvent.click(screen.getByRole("menuitemradio", { name: "العربية" }));
  expect(document.documentElement.lang).toBe("ar");
  expect(screen.queryByRole("menu")).toBeNull();
});

it("opens the shortcuts overlay", () => {
  const show = setup();
  fireEvent.click(screen.getByRole("button", { name: "Shortcuts" }));
  expect(show).toHaveBeenCalled();
});
