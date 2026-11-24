import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { Settings } from "./Settings";
import { emptySettings, profileApi } from "./learner-context";
import { AuthError } from "./session";

vi.mock("../../lib/analytics", () => ({
  setAnalyticsConsent: vi.fn(),
  trackAppearanceChanged: vi.fn(),
  trackPrivacyControl: vi.fn(),
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
      exportData: vi.fn(),
      deleteAccount: vi.fn(),
    },
  };
});

const profile = {
  ...emptySettings(),
  learner_id: "11111111-1111-4111-8111-111111111111",
};
beforeEach(() => {
  window.location.hash = "#privacy";
  localStorage.clear();
  vi.clearAllMocks();
  vi.mocked(profileApi.readSettings).mockResolvedValue(profile);
  vi.mocked(profileApi.saveSettings).mockResolvedValue(profile);
  vi.mocked(profileApi.exportData).mockResolvedValue({
    schema_version: 1,
    exported_at: "2026-11-09T16:00:00.000Z",
    expires_at: "2026-11-16T16:00:00.000Z",
    tables: { learners: [] },
  });
  vi.mocked(profileApi.deleteAccount).mockResolvedValue({ deleted: true });
});

async function open(name: string) {
  render(<Settings />);
  await screen.findByRole("heading", { name: "Settings" });
  fireEvent.click(screen.getByRole("button", { name }));
  return screen.findByRole("dialog");
}

it("export asks for a secure passphrase of 12 characters, then downloads", async () => {
  URL.createObjectURL = vi.fn(() => "blob:x");
  URL.revokeObjectURL = vi.fn();
  const click = vi
    .spyOn(HTMLAnchorElement.prototype, "click")
    .mockImplementation(() => {});
  await open("Export data");
  const field = await screen.findByLabelText("Export passphrase");
  expect(field).toHaveAttribute("type", "password");
  expect(field).toHaveAttribute("autocomplete", "new-password");
  fireEvent.change(field, { target: { value: "short" } });
  fireEvent.submit(field.closest("form")!);
  expect((await screen.findAllByRole("alert")).length).toBeGreaterThan(0);
  expect(profileApi.exportData).not.toHaveBeenCalled();
  fireEvent.change(field, { target: { value: "a-passphrase-at-least-12" } });
  fireEvent.submit(field.closest("form")!);
  fireEvent.click(await screen.findByRole("button", { name: "Download" }));
  await waitFor(() => expect(profileApi.exportData).toHaveBeenCalledOnce());
  await waitFor(() => expect(click).toHaveBeenCalled());
  expect(await screen.findByText(/Encrypted export downloaded/)).toBeVisible();
});

it("delete is confirmed only by the destructive button, never by typing", async () => {
  const dialog = await open("Delete account");
  const confirm = await screen.findByRole("button", { name: "Delete forever" });
  expect(within(dialog).queryByRole("textbox")).toBeNull();
  expect(profileApi.deleteAccount).not.toHaveBeenCalled();
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(profileApi.deleteAccount).toHaveBeenCalledWith("DELETE"),
  );
});

it("cancel leaves without calling anything", async () => {
  await open("Delete account");
  fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(profileApi.deleteAccount).not.toHaveBeenCalled();
});

it("shows the verify link when a recent sign-in is required", async () => {
  vi.mocked(profileApi.deleteAccount).mockRejectedValue(
    new AuthError("Verify your identity.", 403, { code: "reauth_required" }),
  );
  await open("Delete account");
  fireEvent.click(
    await screen.findByRole("button", { name: "Delete forever" }),
  );
  expect(
    await screen.findByRole("link", { name: "Verify your identity" }),
  ).toHaveAttribute("href", "/sign-in?returnTo=%2Fsettings");
});
