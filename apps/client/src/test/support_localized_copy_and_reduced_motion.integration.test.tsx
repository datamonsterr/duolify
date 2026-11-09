import { configureUiAnalytics } from "../lib/analytics";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { UiPreferencesProvider } from "../features/design/localization/Preferences";
import { PreferenceControls } from "../features/design/localization/PreferenceControls";
import { GuidedChatDemo } from "../features/design/chat/GuidedChatDemo";

function mediaDouble() {
  const listeners = new Set<() => void>();
  let reduce = false;
  vi.stubGlobal("matchMedia", (query: string) => ({
    get matches() {
      return query.includes("reduced-motion") ? reduce : false;
    },
    addEventListener: (_: string, listener: () => void) =>
      listeners.add(listener),
    removeEventListener: (_: string, listener: () => void) =>
      listeners.delete(listener),
  }));
  return (value: boolean) =>
    act(() => {
      reduce = value;
      listeners.forEach((fn) => fn());
    });
}
const telemetry = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));
vi.mock("posthog-js/dist/module.no-external", () => ({ default: telemetry }));
afterEach(() => {
  vi.unstubAllGlobals();
  configureUiAnalytics(null);
  vi.clearAllMocks();
});
it("keeps input nodes, focus, quick reply and proposal through live locale and theme changes", () => {
  mediaDouble();
  const view = render(
    <UiPreferencesProvider>
      <GuidedChatDemo />
    </UiPreferencesProvider>,
  );
  const composer = screen.getByRole("textbox", {
    name: "Your answer",
  });
  fireEvent.change(composer, { target: { value: "private learner text" } });
  fireEvent.click(screen.getByRole("button", { name: "City balcony" }));
  composer.focus();
  fireEvent.change(screen.getByLabelText("Language", { exact: true }), {
    target: { value: "ar" },
  });
  expect(composer).toHaveFocus();
  expect(composer).toHaveValue("private learner text");
  expect(view.container.querySelector(".guided-composer textarea")).toBe(
    composer,
  );
  expect(document.documentElement.dir).toBe("rtl");
  expect(view.container.querySelector('[aria-pressed="true"]')).toBeTruthy();
  expect(
    view.container.querySelector(".outline-proposal button"),
  ).toHaveTextContent("تطبيق");
  expect(
    view.container.querySelector(".guided-message strong bdi"),
  ).toHaveTextContent("Duolify");
});
it("honors live OS settings and adapter restore/reset without persisting drafts", () => {
  const system = mediaDouble();
  const adapter = {
    load: () => ({ locale: "ar", appearance: "dark", reduceMotion: false }),
    save: vi.fn(),
    clear: vi.fn(),
  };
  const view = render(
    <UiPreferencesProvider adapter={adapter}>
      <PreferenceControls />
    </UiPreferencesProvider>,
  );
  expect(document.documentElement.dataset.theme).toBe("dark");
  expect(document.documentElement.dataset.motion).toBe("full");
  system(true);
  expect(document.documentElement.dataset.motion).toBe("reduce");
  fireEvent.click(view.container.querySelector('[data-control="reset"]')!);
  expect(adapter.clear).toHaveBeenCalledOnce();
  expect(document.documentElement.lang).toBe("en");
  expect(document.documentElement.dataset.motion).toBe("reduce");
  fireEvent.change(screen.getByLabelText("Appearance", { exact: true }), {
    target: { value: "dark" },
  });
  expect(adapter.save).toHaveBeenLastCalledWith({
    locale: "en",
    appearance: "dark",
    reduceMotion: false,
  });
  view.unmount();
  render(
    <UiPreferencesProvider>
      <PreferenceControls />
    </UiPreferencesProvider>,
  );
  expect(document.documentElement.dataset.theme).toBe("light");
});

it("keeps pricing selection while formatting API prices and dates in the chosen locale", async () => {
  mediaDouble();
  const { QueryClient, QueryClientProvider } =
    await import("@tanstack/react-query");
  const { PricingView } = await import("../features/pricing/PricingView");
  const { server } = await import("./setup");
  const { http, HttpResponse } = await import("msw");
  const pricing = (await import("../features/pricing/fixtures/approved.json"))
    .default;
  server.use(http.get("*/v1/pricing", () => HttpResponse.json(pricing)));
  render(
    <QueryClientProvider client={new QueryClient()}>
      <UiPreferencesProvider>
        <PreferenceControls />
        <PricingView />
      </UiPreferencesProvider>
    </QueryClientProvider>,
  );
  await screen.findByText(/daily free credits/);
  fireEvent.click(screen.getByRole("button", { name: "Browse prices" }));
  const offer = screen.getAllByRole("radio")[0];
  fireEvent.click(offer);
  fireEvent.change(screen.getByLabelText("Language", { exact: true }), {
    target: { value: "ar" },
  });
  expect(offer).toBeChecked();
  expect(
    screen.getByText(
      new Intl.NumberFormat("ar", {
        style: "currency",
        currency: "USD",
      }).format(pricing.packs[0].amount_minor / 100),
      { normalizer: (value) => value },
    ),
  ).toBeVisible();
  expect(screen.getByText(/Approved pricing/)).toHaveTextContent(
    new Intl.DateTimeFormat("ar", {
      dateStyle: "medium",
      timeZone: "UTC",
    }).format(new Date(pricing.effective_from)),
  );
});

it("keeps preferences usable after adapter failures and retries without copying learner content", () => {
  mediaDouble();
  const adapter = {
    load: () => null,
    save: vi.fn((): void => {
      throw new Error("offline");
    }),
    clear: vi.fn(),
  };
  render(
    <UiPreferencesProvider adapter={adapter}>
      <PreferenceControls />
    </UiPreferencesProvider>,
  );
  fireEvent.change(screen.getByLabelText("Appearance", { exact: true }), {
    target: { value: "dark" },
  });
  expect(screen.getByRole("alert")).toHaveTextContent(
    "Could not save preferences.",
  );
  expect(document.documentElement.dataset.theme).toBe("dark");
  adapter.save.mockImplementation(() => {});
  fireEvent.click(
    screen.getByRole("button", { name: "Retry saving preferences" }),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(adapter.save).toHaveBeenLastCalledWith({
    appearance: "dark",
    locale: "en",
    reduceMotion: false,
  });
});

it("translates a visible outline reorder status when language changes without losing undo history", () => {
  mediaDouble();
  const view = render(
    <UiPreferencesProvider>
      <GuidedChatDemo />
    </UiPreferencesProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Move Planets up" }));
  expect(
    view.container.querySelector('.outline-editor [role="status"]'),
  ).toHaveTextContent("Planets moved to position 2");
  fireEvent.change(screen.getByLabelText("Language", { exact: true }), {
    target: { value: "ar" },
  });
  expect(
    view.container.querySelector('.outline-editor [role="status"]'),
  ).toHaveTextContent("نُقل");
  fireEvent.click(screen.getByRole("button", { name: "تراجع" }));
  expect(view.container.querySelectorAll(".outline-unit")[2]).toHaveAttribute(
    "data-outline-id",
    "planets",
  );
});

it("isolates real course names and localizes progress plurals without changing destinations", async () => {
  mediaDouble();
  const { CourseCard } = await import("../features/design/CourseCard");
  const view = render(
    <UiPreferencesProvider>
      <PreferenceControls />
      <CourseCard
        course={{
          id: "stable-id",
          title: "ليلى — Astronomy",
          description: "Public description",
          units: [
            {
              title: "Unit",
              lessons: [{ id: "one", title: "One", state: "done" }],
            },
          ],
        }}
      />
    </UiPreferencesProvider>,
  );
  expect(view.container.querySelector("h2 bdi")).toHaveTextContent("ليلى");
  expect(screen.getByRole("link")).toHaveAttribute("href", "/course/stable-id");
  expect(screen.getByText("1 of 1 lesson completed")).toBeVisible();
  fireEvent.change(screen.getByLabelText("Language", { exact: true }), {
    target: { value: "ar" },
  });
  expect(screen.getByRole("link")).toHaveAttribute("href", "/course/stable-id");
  expect(screen.getByRole("progressbar")).toHaveAccessibleName(/ليلى/);
});

it("delivers content-free control events through PostHog and respects consent", () => {
  mediaDouble();
  configureUiAnalytics({
    learnerId: "10000000-0000-4000-8000-000000000001",
    platform: "web",
    appVersion: "test",
    enabled: true,
  });
  render(
    <UiPreferencesProvider>
      <GuidedChatDemo />
    </UiPreferencesProvider>,
  );
  fireEvent.change(screen.getByRole("textbox", { name: "Your answer" }), {
    target: { value: "PRIVATE chat answer" },
  });
  fireEvent.change(screen.getByLabelText("Appearance", { exact: true }), {
    target: { value: "dark" },
  });
  expect(telemetry.capture).toHaveBeenCalledWith("ui_appearance_changed", {
    distinct_id: "10000000-0000-4000-8000-000000000001",
    platform: "web",
    app_version: "test",
    source: "client",
    mode: "dark",
  });
  fireEvent.click(screen.getByLabelText("Reduce motion", { exact: true }));
  expect(telemetry.capture).toHaveBeenCalledWith(
    "ui_control_activated",
    expect.objectContaining({ surface: "settings", control_id: "motion" }),
  );
  expect(JSON.stringify(telemetry.capture.mock.calls)).not.toContain("PRIVATE");
  telemetry.capture.mockClear();
  configureUiAnalytics({
    learnerId: "10000000-0000-4000-8000-000000000001",
    platform: "web",
    appVersion: "test",
    enabled: false,
  });
  fireEvent.change(screen.getByLabelText("Appearance", { exact: true }), {
    target: { value: "light" },
  });
  expect(telemetry.capture).not.toHaveBeenCalled();
});

it("uses the E1 account locale as the initial and reset language without persisting device preferences", () => {
  mediaDouble();
  const view = render(
    <UiPreferencesProvider initialPreferences={{ locale: "ar" }}>
      <PreferenceControls />
    </UiPreferencesProvider>,
  );
  expect(document.documentElement.lang).toBe("ar");
  fireEvent.change(view.container.querySelector('[data-control="locale"]')!, {
    target: { value: "en" },
  });
  expect(document.documentElement.lang).toBe("en");
  fireEvent.click(view.container.querySelector('[data-control="reset"]')!);
  expect(document.documentElement.lang).toBe("ar");
});
