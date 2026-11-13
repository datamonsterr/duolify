import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, afterEach } from "vitest";
import { CourseLevelOverride } from "./CourseLevelOverride";
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

describe("CourseLevelOverride", () => {
  it("renders 'Uses global' when level_override is empty and allows setting an override", async () => {
    const onSave = vi.fn().mockResolvedValue({ level_override: "advanced" });
    render(
      <CourseLevelOverride
        courseId="c1"
        courseTitle="Astronomy"
        levelOverride=""
        globalLevel="beginner"
        onSaveLevel={onSave}
      />,
    );

    expect(screen.getByText("Astronomy")).toBeInTheDocument();
    expect(screen.getByText(/Uses global.*beginner/i)).toBeInTheDocument();

    const overrideBtn = screen.getByRole("button", {
      name: /Override level for Astronomy/i,
    });
    fireEvent.click(overrideBtn);

    const select = screen.getByLabelText(/Level for Astronomy/i);
    fireEvent.change(select, { target: { value: "advanced" } });

    const saveBtn = screen.getByRole("button", {
      name: /Save level for Astronomy/i,
    });
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith("c1", "advanced");
    });
  });

  it("renders current override and allows resetting back to global", async () => {
    const onSave = vi.fn().mockResolvedValue({ level_override: "" });
    render(
      <CourseLevelOverride
        courseId="c2"
        courseTitle="Plant Biology"
        levelOverride="intermediate"
        globalLevel="beginner"
        onSaveLevel={onSave}
      />,
    );

    expect(screen.getByText("Plant Biology")).toBeInTheDocument();
    expect(screen.getByText(/Intermediate/i)).toBeInTheDocument();

    const resetBtn = screen.getByRole("button", {
      name: /Reset level for Plant Biology/i,
    });
    fireEvent.click(resetBtn);

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith("c2", "");
    });
  });

  it("handles save failures gracefully without crashing", async () => {
    const onSave = vi.fn().mockRejectedValue(new Error("Network failure"));
    render(
      <CourseLevelOverride
        courseId="c1"
        courseTitle="Astronomy"
        levelOverride=""
        globalLevel="beginner"
        onSaveLevel={onSave}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: /Override level for Astronomy/i }),
    );
    fireEvent.change(screen.getByLabelText(/Level for Astronomy/i), {
      target: { value: "expert" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: /Save level for Astronomy/i }),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Network failure/i,
    );
  });

  it("tracks course controls without private text and respects opt-out", () => {
    configureUiAnalytics({
      learnerId: "11111111-1111-4111-8111-111111111111",
      platform: "web",
      appVersion: "0.1.0",
      enabled: true,
    });

    trackControl("open_course", "c1");
    expect(posthog.capture).toHaveBeenCalledWith("ui_control_activated", {
      distinct_id: "11111111-1111-4111-8111-111111111111",
      platform: "web",
      app_version: "0.1.0",
      source: "client",
      surface: "courses",
      control_id: "open_course",
      course_id: "c1",
    });

    posthog.capture.mockClear();

    // With opt-out
    configureUiAnalytics({
      learnerId: "11111111-1111-4111-8111-111111111111",
      platform: "web",
      appVersion: "0.1.0",
      enabled: false,
    });

    trackControl("open_course", "c1");
    expect(posthog.capture).not.toHaveBeenCalled();
  });
});
