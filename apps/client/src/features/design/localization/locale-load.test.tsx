import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { UiPreferencesProvider } from "./Preferences";
import { PreferenceControls } from "./PreferenceControls";

const loading = vi.hoisted(() => ({ ready: false, load: vi.fn() }));
vi.mock("./messages", async (original) => {
  const actual = await original<typeof import("./messages")>();
  return {
    ...actual,
    arabicLoaded: () => loading.ready,
    loadArabic: loading.load,
  };
});
it("keeps the previous language after a catalogue failure and retries", async () => {
  loading.load
    .mockRejectedValueOnce(new Error("offline"))
    .mockImplementationOnce(async () => {
      loading.ready = true;
    });
  render(
    <UiPreferencesProvider>
      <PreferenceControls />
    </UiPreferencesProvider>,
  );
  fireEvent.change(screen.getByLabelText("Language"), {
    target: { value: "ar" },
  });
  await screen.findByText("Could not load the language. Try again.");
  expect(document.documentElement.lang).toBe("en");
  expect(document.documentElement.dir).toBe("ltr");
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await waitFor(() => expect(document.documentElement.lang).toBe("ar"));
  expect(document.documentElement.dir).toBe("rtl");
  expect(loading.load).toHaveBeenCalledTimes(2);
});
