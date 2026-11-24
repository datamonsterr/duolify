import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { Settings } from "./Settings";
import { emptySettings, profileApi } from "./learner-context";

vi.mock("./learner-context", async (load) => {
  const actual = await load<typeof import("./learner-context")>();
  return {
    ...actual,
    profileApi: { ...actual.profileApi, readSettings: vi.fn() },
  };
});
beforeEach(() => {
  window.location.hash = "";
  vi.mocked(profileApi.readSettings).mockResolvedValue({
    ...emptySettings(),
    learner_id: "11111111-1111-4111-8111-111111111111",
  });
});

it("shows one section at a time under a tab row, with no duplicate Profile link", async () => {
  render(<Settings />);
  const tabs = await screen.findByRole("navigation", { name: "Settings" });
  expect(within(tabs).getAllByRole("link")).toHaveLength(5);
  expect(
    within(tabs).getByRole("link", { name: "Look and language" }),
  ).toHaveAttribute("aria-current", "page");
  expect(screen.getByLabelText("Reduce motion")).toBeVisible();
  expect(
    screen.getByRole("button", { name: "Reset preferences" }),
  ).toBeVisible();
  expect(screen.queryByLabelText("Analytics")).not.toBeVisible();
  window.location.hash = "#privacy";
  fireEvent(window, new HashChangeEvent("hashchange"));
  expect(await screen.findByLabelText("Analytics")).toBeVisible();
  expect(screen.queryByRole("link", { name: "Profile" })).toBeNull();
});
