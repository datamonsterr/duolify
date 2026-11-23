import { beforeEach, describe, expect, it, vi } from "vitest";
import posthog from "posthog-js/dist/module.no-external";
import { configureAnalytics, trackRegion } from "../../../lib/analytics";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
  },
}));

const learnerId = "a1111111-1111-4111-8111-111111111111";
const on = (optedOut = false) =>
  configureAnalytics({
    learnerId,
    platform: "web",
    appVersion: "9.9.9",
    optedOut,
  });
const base = { course_id: "c1", lesson_id: "l1", slide_index: 2 };

describe("region question analytics", () => {
  beforeEach(() => {
    vi.mocked(posthog.capture).mockClear();
    on();
  });

  it("lesson_region_selected carries ids, the method and an area bucket, never coordinates", () => {
    trackRegion({
      event: "lesson_region_selected",
      ...base,
      method: "keyboard",
      area: 0.16,
    });
    expect(posthog.capture).toHaveBeenCalledWith("lesson_region_selected", {
      distinct_id: learnerId,
      platform: "web",
      app_version: "9.9.9",
      source: "client",
      ...base,
      method: "keyboard",
      area_bucket: "medium",
    });
  });

  it("buckets the area", () => {
    const bucket = (area: number) => {
      vi.mocked(posthog.capture).mockClear();
      trackRegion({
        event: "lesson_region_selected",
        ...base,
        method: "two_finger",
        area,
      });
      return vi.mocked(posthog.capture).mock.calls[0][1]!.area_bucket;
    };
    expect([0.01, 0.07, 0.1, 0.29, 0.3, 1].map(bucket)).toEqual([
      "small",
      "small",
      "medium",
      "medium",
      "large",
      "large",
    ]);
  });

  it("lesson_answer_panel_closed carries the outcome, saved_note and a follow-up bucket", () => {
    const cases: [number, string][] = [
      [0, "0"],
      [1, "1"],
      [2, "2_plus"],
      [9, "2_plus"],
    ];
    for (const [n, bucket] of cases) {
      vi.mocked(posthog.capture).mockClear();
      trackRegion({
        event: "lesson_answer_panel_closed",
        ...base,
        outcome: "stopped",
        saved_note: true,
        follow_ups: n,
      });
      expect(posthog.capture).toHaveBeenCalledWith(
        "lesson_answer_panel_closed",
        {
          distinct_id: learnerId,
          platform: "web",
          app_version: "9.9.9",
          source: "client",
          ...base,
          outcome: "stopped",
          saved_note: true,
          follow_ups: bucket,
        },
      );
    }
  });

  it("emits nothing for an opted-out Learner, an unknown identity or a malformed id", () => {
    on(true);
    trackRegion({
      event: "lesson_region_selected",
      ...base,
      method: "select_area",
      area: 0.2,
    });
    on();
    trackRegion({
      event: "lesson_region_selected",
      ...base,
      course_id: "has space/free text",
      method: "select_area",
      area: 0.2,
    });
    trackRegion({
      event: "lesson_region_selected",
      ...base,
      slide_index: -1,
      method: "select_area",
      area: 0.2,
    });
    configureAnalytics(null);
    trackRegion({
      event: "lesson_region_selected",
      ...base,
      method: "select_area",
      area: 0.2,
    });
    expect(posthog.capture).not.toHaveBeenCalled();
  });
});
