import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import tokens from "../../../../../docs/design/tokens.json";
import { Button } from "./Button";
import { CourseCard } from "./CourseCard";
import { Badge } from "./Badge";
import { configureUiAnalytics, trackControl } from "../../lib/analytics";

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
function luminance(hex: string) {
  return hex
    .match(/[a-f\d]{2}/gi)!
    .map((part) => parseInt(part, 16) / 255)
    .map((v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4))
    .reduce((sum, v, i) => sum + v * [0.2126, 0.7152, 0.0722][i], 0);
}
for (const mode of ["light", "dark"] as const) {
  it(`preserves every permitted text/feedback/control contrast pair in ${mode}`, () => {
    const colors: Record<string, { $value: { hex: string } }> =
      tokens.color[mode];
    const pairs = [
      ...["canvas", "surface", "raised"].flatMap((bg) =>
        ["text", "muted", "primary"].map((fg) => [fg, bg, 4.5] as const),
      ),
      ...["canvas", "surface", "raised"].flatMap((bg) =>
        ["line", "focus"].map((fg) => [fg, bg, 3] as const),
      ),
      ["on-primary", "primary", 4.5],
      ["on-primary", "primary-hover", 4.5],
      ["on-secondary", "secondary", 4.5],
      ["on-accent", "accent", 4.5],
      ...["success", "danger", "warning", "info", "credit", "streak"].map(
        (role) => [role, `${role}-bg`, 4.5] as const,
      ),
    ] as const;
    for (const [fg, bg, min] of pairs) {
      const a = luminance(colors[fg].$value.hex),
        b = luminance(colors[bg].$value.hex);
      expect(
        (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05),
        `${fg} / ${bg}`,
      ).toBeGreaterThanOrEqual(min);
    }
  });
}

describe("course foundation boundaries", () => {
  it("uses text-free lime progress and 72px path nodes", () => {
    const css = readFileSync(
      resolve("src/features/design", "../../styles.css"),
      "utf8",
    );
    expect(css).toContain("inline-size: var(--size-node)");
    expect(css).toContain("block-size: var(--size-node)");
    // Lime may fill progress or the explicitly hidden geometric brand, never text containers.
    const limeRules = [
      ...css.matchAll(/([^{}]+)\{([^{}]*var\(--lime(?:-edge)?\)[^{}]*)\}/g),
    ];
    expect(limeRules.length).toBeGreaterThan(0);
    for (const rule of limeRules)
      expect(rule[1].trim()).toMatch(/^(progress(?:::|-moz-)|\.brand-mark)/);
    expect(tokens.size.node.$value.value).toBe(72);
  });
  it("prevents duplicate pending actions and preserves a named unavailable state", () => {
    const activate = vi.fn();
    const { rerender } = render(
      <Button pending onClick={activate}>
        Refresh courses
      </Button>,
    );
    const button = screen.getByRole("button", { name: "Preparing…" });
    expect(button).toHaveAttribute("aria-busy", "true");
    fireEvent.click(button);
    expect(activate).not.toHaveBeenCalled();
    rerender(
      <Button disabled aria-describedby="why">
        Create a course
      </Button>,
    );
    expect(
      screen.getByRole("button", { name: "Create a course" }),
    ).toBeDisabled();
  });
  it("renders empty and unavailable cards without invented progress or a destination", () => {
    const course = {
      id: "empty",
      title: "Long Unicode course 日本語 العربية Tiếng Việt",
      description: "A course",
      units: [],
      availability: "unavailable" as const,
    };
    render(<CourseCard course={course} />);
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText("Course unavailable")).toBeVisible();
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  });
  it("keeps semantic badge foreground/background roles paired", () => {
    render(
      <>
        <Badge role="credit">Credits</Badge>
        <Badge role="streak">Streaks</Badge>
        <Badge role="info">Current</Badge>
      </>,
    );
    for (const role of ["credit", "streak", "info"])
      expect(document.querySelector(`.badge-${role}`)).toBeTruthy();
  });
  it("drops UI events without an enabled UUID learner context, without blocking controls", () => {
    configureUiAnalytics(null);
    trackControl("refresh_courses");
    expect(posthog.capture).not.toHaveBeenCalled();
    configureUiAnalytics({
      learnerId: "email@example.com",
      platform: "web",
      appVersion: "0.1.0",
      enabled: true,
    });
    trackControl("refresh_courses");
    expect(posthog.capture).not.toHaveBeenCalled();
    configureUiAnalytics(null);
  });
});

it("emits stable course controls with common properties and survives transport failure", () => {
  const context = {
    learnerId: "56d4e614-7665-4f14-b83e-8b5e9bed384a",
    platform: "web" as const,
    appVersion: "0.1.0",
    enabled: true,
  };
  configureUiAnalytics(context);
  for (const control of [
    "open_course",
    "refresh_courses",
    "retry_courses",
    "clear_search",
  ] as const) {
    trackControl(control, control === "open_course" ? "astronomy" : undefined);
    expect(posthog.capture).toHaveBeenLastCalledWith("ui_control_activated", {
      distinct_id: context.learnerId,
      platform: "web",
      app_version: "0.1.0",
      source: "client",
      surface: "courses",
      control_id: control,
      ...(control === "open_course" ? { course_id: "astronomy" } : {}),
    });
  }
  posthog.capture.mockClear();
  configureUiAnalytics({ ...context, enabled: false });
  trackControl("open_course", "astronomy");
  expect(posthog.capture).not.toHaveBeenCalled();
  configureUiAnalytics(context);
  posthog.capture.mockImplementationOnce(() => {
    throw new Error("unavailable");
  });
  expect(() => trackControl("retry_courses")).not.toThrow();
});
