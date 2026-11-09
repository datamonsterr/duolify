import { readFileSync } from "node:fs";
import { expect, it, vi, afterEach } from "vitest";
import {
  navigation,
  mobileTabs,
  moreDestinations,
  currentDestination,
  screenForPath,
} from "../shell/navigation";
import {
  configureUiAnalytics,
  trackNavigation,
  trackScreen,
} from "../../lib/analytics";

const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));
vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));
afterEach(() => {
  configureUiAnalytics(null);
  vi.clearAllMocks();
});

it("maps the same destination set and marks descendant pages without matching unrelated prefixes", () => {
  expect(
    new Set(
      [...mobileTabs.filter((d) => d.id !== "more"), ...moreDestinations].map(
        (d) => d.id,
      ),
    ),
  ).toEqual(new Set(navigation.map((d) => d.id)));
  expect(currentDestination("/notes/n_42")).toBe("notes");
  expect(currentDestination("/course/astronomy")).toBe("learn");
  expect(currentDestination("/credits/shop", true)).toBe("more");
  expect(currentDestination("/courses/new")).toBe("courses");
  expect(currentDestination("/notes-other")).toBeUndefined();
  expect(screenForPath("/lesson/private-topic/test")).toBe("lesson_test");
  expect(screenForPath("/private-unknown")).toBe("not_found");
});

it("delivers only catalogue enums/common properties and suppresses invalid identity, opt-out and transport failure", () => {
  const context = {
    learnerId: "c94d6abe-40d0-4d5a-8eb8-9e53c4fb9856",
    enabled: true,
    platform: "desktop" as const,
    appVersion: "test",
  };
  trackNavigation("courses");
  expect(posthog.capture).not.toHaveBeenCalled();
  configureUiAnalytics({ ...context, learnerId: "person@example.com" });
  trackScreen("courses");
  expect(posthog.capture).not.toHaveBeenCalled();
  configureUiAnalytics(context);
  trackNavigation("courses");
  trackScreen("lesson_test");
  expect(posthog.capture.mock.calls).toEqual([
    [
      "ui_navigation_selected",
      {
        distinct_id: context.learnerId,
        platform: "desktop",
        app_version: "test",
        source: "client",
        destination: "courses",
      },
    ],
    [
      "ui_screen_viewed",
      {
        distinct_id: context.learnerId,
        platform: "desktop",
        app_version: "test",
        source: "client",
        screen_id: "lesson_test",
      },
    ],
  ]);
  // Runtime callers cannot smuggle free text through a type assertion.
  trackScreen("private search" as "courses");
  trackNavigation("private topic" as "courses");
  expect(posthog.capture).toHaveBeenCalledTimes(2);
  configureUiAnalytics({ ...context, enabled: false });
  trackScreen("courses");
  expect(posthog.capture).toHaveBeenCalledTimes(2);
  configureUiAnalytics(context);
  posthog.has_opted_out_capturing.mockReturnValueOnce(true);
  trackNavigation("courses");
  expect(posthog.capture).toHaveBeenCalledTimes(2);
  posthog.capture.mockImplementationOnce(() => {
    throw Error("offline");
  });
  expect(() => trackNavigation("more")).not.toThrow();
});

it("keeps documented breakpoint, dynamic tab reservation, safe-area and instant-motion contracts", () => {
  const css = readFileSync("src/features/shell/shell.css", "utf8");
  expect(css).toContain("48rem");
  expect(css).toContain("75rem");
  expect(css).toContain("env(safe-area-inset-bottom)");
  expect(css).toContain("grid-template-rows: auto minmax(0, 1fr) auto");
  expect(css).toContain("scroll-behavior: auto");
});
