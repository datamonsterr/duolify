import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { Settings } from "./Settings";
import { emptySettings, profileApi } from "./learner-context";

vi.mock("../../lib/analytics", () => ({
  setAnalyticsConsent: vi.fn(),
  trackAppearanceChanged: vi.fn(),
  trackPrivacyControl: vi.fn(),
  trackPreference: vi.fn(),
  trackGuidedChatEvent: vi.fn(),
}));
vi.mock("./learner-context", async (load) => {
  const actual = await load<typeof import("./learner-context")>();
  return {
    ...actual,
    profileApi: {
      ...actual.profileApi,
      readSettings: vi.fn(),
      saveSettings: vi.fn(),
    },
  };
});

const settings = {
  ...emptySettings(),
  learner_id: "11111111-1111-4111-8111-111111111111",
  daily_minutes: 20,
  daily_xp: 60,
  reminder_enabled: true,
  reminder_time: "07:30",
};
beforeEach(() => {
  localStorage.clear();
  vi.clearAllMocks();
  vi.mocked(profileApi.readSettings).mockResolvedValue(settings);
});

it("daily goal and reminder open as a chat dialog when the flag is on", async () => {
  render(<Settings />);
  await screen.findByRole("heading", { name: "Settings" });
  expect(screen.queryByLabelText("Daily XP goal")).toBeNull();
  fireEvent.click(await screen.findByRole("button", { name: /^Daily goal$/ }));
  const dialog = await screen.findByRole("dialog", { name: "Daily goal" });
  expect(dialog.textContent).toContain("Save your daily goal?");
});

it("keeps the old fields behind the flag for rollback", async () => {
  localStorage.setItem("duolify.guided.rest", "off");
  render(<Settings />);
  expect(await screen.findByLabelText("Daily XP goal")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Daily goal" })).toBeNull();
});
