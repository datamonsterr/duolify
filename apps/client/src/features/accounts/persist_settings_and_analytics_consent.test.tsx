import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { Settings } from "./Settings";
import { emptySettings, profileApi } from "./learner-context";
import { UiPreferencesProvider } from "../design/localization/Preferences";

const analytics = vi.hoisted(() => ({
  setAnalyticsConsent: vi.fn(),
  trackAppearanceChanged: vi.fn(),
}));
vi.mock("../../lib/analytics", () => analytics);
vi.mock("./learner-context", async (load) => {
  const actual = await load<typeof import("./learner-context")>();
  return {
    ...actual,
    profileApi: {
      read: vi.fn(),
      commit: vi.fn(),
      readSettings: vi.fn(),
      saveSettings: vi.fn(),
    },
  };
});

const profile = {
  ...emptySettings(),
  learner_id: "11111111-1111-4111-8111-111111111111",
};

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.removeItem("duolify.analytics-opt-out");
  vi.mocked(profileApi.readSettings).mockResolvedValue(profile);
  vi.mocked(profileApi.saveSettings).mockImplementation(async (fields) => ({
    ...profile,
    ...fields,
  }));
});

it("loads saved settings, persists changes, and applies analytics consent", async () => {
  render(<Settings />);
  await screen.findByRole("heading", { name: "Settings" });
  expect(await screen.findByLabelText("Appearance")).toHaveValue("system");
  fireEvent.change(screen.getByLabelText("Appearance"), {
    target: { value: "dark" },
  });
  fireEvent.click(screen.getByLabelText("Analytics"));
  fireEvent.click(screen.getByRole("button", { name: "Save settings" }));
  await waitFor(() =>
    expect(profileApi.saveSettings).toHaveBeenCalledWith(
      expect.objectContaining({ appearance: "dark", analytics_opt_out: true }),
    ),
  );
  expect(analytics.setAnalyticsConsent).toHaveBeenCalledWith(false);
  expect(analytics.trackAppearanceChanged).not.toHaveBeenCalled();
  expect(await screen.findByText("Settings saved")).toBeVisible();
});

it("emits only the stable appearance value after an opted-in save", async () => {
  render(<Settings />);
  await screen.findByRole("heading", { name: "Settings" });
  fireEvent.change(await screen.findByLabelText("Appearance"), {
    target: { value: "dark" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save settings" }));
  await waitFor(() =>
    expect(analytics.trackAppearanceChanged).toHaveBeenCalledWith("dark"),
  );
});

it("saves the localized preferences through account settings", async () => {
  render(
    <UiPreferencesProvider>
      <Settings />
    </UiPreferencesProvider>,
  );
  await screen.findByRole("heading", { name: "Settings" });
  fireEvent.change(screen.getByLabelText("Language"), {
    target: { value: "ar" },
  });
  fireEvent.click(screen.getByLabelText("Reduce motion"));
  fireEvent.click(screen.getByRole("button", { name: "Save settings" }));
  await waitFor(() =>
    expect(profileApi.saveSettings).toHaveBeenCalledWith(
      expect.objectContaining({ locale: "ar", motion: "reduce" }),
    ),
  );
  expect(screen.getByRole("link", { name: "Manage sessions" })).toHaveAttribute(
    "href",
    "/settings/sessions",
  );
});

it("translates the settings heading and sessions link for Arabic accounts", async () => {
  render(
    <UiPreferencesProvider initialPreferences={{ locale: "ar" }}>
      <Settings />
    </UiPreferencesProvider>,
  );
  await screen.findByRole("heading", { name: "الإعدادات" });
  expect(screen.getByRole("link", { name: "إدارة الجلسات" })).toHaveAttribute(
    "href",
    "/settings/sessions",
  );
  expect(screen.getByLabelText("المظهر")).toBeVisible();
});
