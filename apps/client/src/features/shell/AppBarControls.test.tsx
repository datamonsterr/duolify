import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { UiPreferencesProvider } from "../design/localization/Preferences";
import { AppBarControls } from "./AppBarControls";

it("switches language and theme from the app bar", () => {
  render(
    <UiPreferencesProvider>
      <AppBarControls />
    </UiPreferencesProvider>,
  );
  const theme = screen.getByRole("button", { name: "Dark theme" });
  expect(theme).toHaveAttribute("aria-pressed", "false");
  fireEvent.click(theme);
  expect(theme).toHaveAttribute("aria-pressed", "true");
  expect(document.documentElement.dataset.theme).toBe("dark");
  fireEvent.click(theme);
  expect(theme).toHaveAttribute("aria-pressed", "false");
  fireEvent.change(screen.getByLabelText("Language"), {
    target: { value: "ar" },
  });
  expect(document.documentElement.lang).toBe("ar");
});
