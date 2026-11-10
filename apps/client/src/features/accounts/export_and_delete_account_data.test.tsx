import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { Settings } from "./Settings";
import { emptySettings, profileApi } from "./learner-context";
import { encryptAccountExport } from "./encrypted-export";
import { AuthError } from "./session";

const analytics = vi.hoisted(() => ({
  setAnalyticsConsent: vi.fn(),
  trackAppearanceChanged: vi.fn(),
  trackPrivacyControl: vi.fn(),
}));
vi.mock("../../lib/analytics", () => analytics);
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

it("downloads encrypted export data through the privacy control", async () => {
  render(<Settings />);
  await screen.findByRole("heading", { name: "Settings" });
  fireEvent.click(screen.getByRole("button", { name: "Export data" }));
  expect(await screen.findByLabelText("Export passphrase")).toBeVisible();
  fireEvent.change(screen.getByLabelText("Export passphrase"), {
    target: { value: "a-private-passphrase" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Download encrypted export" }),
  );
  await waitFor(() => expect(profileApi.exportData).toHaveBeenCalled());
  expect(analytics.trackPrivacyControl).toHaveBeenCalledWith("export_data");
});

it("requires explicit deletion confirmation and calls the deletion request", async () => {
  render(<Settings />);
  await screen.findByRole("heading", { name: "Settings" });
  fireEvent.click(screen.getByRole("button", { name: "Delete account" }));
  expect(
    screen.getByRole("dialog", { name: "Delete your account?" }),
  ).toBeVisible();
  fireEvent.change(screen.getByLabelText("Type DELETE to confirm"), {
    target: { value: "DELETE" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Delete permanently" }));
  await waitFor(() =>
    expect(profileApi.deleteAccount).toHaveBeenCalledWith("DELETE"),
  );
  expect(analytics.trackPrivacyControl).toHaveBeenCalledWith("delete_account");
});

it("cancels export and deletion dialogs without calling mutations", async () => {
  render(<Settings />);
  await screen.findByRole("heading", { name: "Settings" });

  fireEvent.click(screen.getByRole("button", { name: "Export data" }));
  expect(
    screen.getByRole("dialog", { name: "Prepare your export" }),
  ).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(
    screen.queryByRole("dialog", { name: "Prepare your export" }),
  ).toBeNull();
  expect(profileApi.exportData).not.toHaveBeenCalled();

  fireEvent.click(screen.getByRole("button", { name: "Delete account" }));
  expect(
    screen.getByRole("dialog", { name: "Delete your account?" }),
  ).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(
    screen.queryByRole("dialog", { name: "Delete your account?" }),
  ).toBeNull();
  expect(profileApi.deleteAccount).not.toHaveBeenCalled();
});

it("validates passphrase length for encrypted exports", async () => {
  await expect(encryptAccountExport({ test: true }, "short")).rejects.toThrow(
    "Use at least 12 characters.",
  );

  const result = await encryptAccountExport(
    { test: true },
    "a-passphrase-at-least-12",
  );
  expect(result).toContain("duolify-encrypted-export-v1");
  expect(result).toContain("PBKDF2-SHA256/AES-256-GCM");
});

it.each(["export", "delete"])(
  "asks for recent reauthentication when %s is denied",
  async (action) => {
    vi.mocked(profileApi.exportData).mockRejectedValue(
      new AuthError("Verify your identity.", 403, { code: "reauth_required" }),
    );
    vi.mocked(profileApi.deleteAccount).mockRejectedValue(
      new AuthError("Verify your identity.", 403, { code: "reauth_required" }),
    );
    render(<Settings />);
    await screen.findByRole("heading", { name: "Settings" });
    fireEvent.click(
      screen.getByRole("button", {
        name: action === "export" ? "Export data" : "Delete account",
      }),
    );
    if (action === "export")
      fireEvent.change(screen.getByLabelText("Export passphrase"), {
        target: { value: "a-private-export-passphrase" },
      });
    else
      fireEvent.change(screen.getByLabelText("Type DELETE to confirm"), {
        target: { value: "DELETE" },
      });
    fireEvent.click(
      screen.getByRole("button", {
        name:
          action === "export"
            ? "Download encrypted export"
            : "Delete permanently",
      }),
    );
    expect(
      await screen.findByRole("link", { name: "Verify your identity" }),
    ).toHaveAttribute("href", "/sign-in?returnTo=%2Fsettings");
  },
);
