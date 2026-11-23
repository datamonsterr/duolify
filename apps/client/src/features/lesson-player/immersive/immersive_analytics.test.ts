import { appVersion } from "../../../lib/client-build";
import { beforeEach, describe, expect, it, vi } from "vitest";
import posthog from "posthog-js/dist/module.no-external";
import {
  configureAnalytics,
  secondsInModeBucket,
  trackImmersive,
} from "../../../lib/analytics";

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
  configureAnalytics({ learnerId, platform: "android", optedOut });

describe("immersive analytics", () => {
  beforeEach(() => {
    vi.mocked(posthog.capture).mockClear();
    on();
  });
  it("emits entered with ids, slide index and trigger only", () => {
    trackImmersive({
      event: "immersive_mode_entered",
      course_id: "c1",
      lesson_id: "l1",
      slide_index: 2,
      trigger: "rotation",
    });
    expect(posthog.capture).toHaveBeenCalledWith("immersive_mode_entered", {
      distinct_id: learnerId,
      platform: "android",
      app_version: appVersion,
      source: "client",
      course_id: "c1",
      lesson_id: "l1",
      slide_index: 2,
      trigger: "rotation",
    });
  });
  it("buckets seconds in mode on exit", () => {
    trackImmersive({
      event: "immersive_mode_exited",
      course_id: "c1",
      lesson_id: "l1",
      slide_index: 0,
      trigger: "back",
      seconds_in_mode: 45,
    });
    expect(vi.mocked(posthog.capture).mock.calls[0][1]).toMatchObject({
      seconds_in_mode: "30_120",
      trigger: "back",
    });
    expect(
      [5, 10, 29, 30, 119, 120, 599, 600, 5000].map(secondsInModeBucket),
    ).toEqual([
      "lt_10",
      "10_30",
      "10_30",
      "30_120",
      "30_120",
      "120_600",
      "120_600",
      "gt_600",
      "gt_600",
    ]);
  });
  it("emits control use with closed enums and no ids", () => {
    trackImmersive({
      event: "immersive_control_used",
      control: "replay_slide",
      via: "double_tap",
    });
    const props = vi.mocked(posthog.capture).mock.calls[0][1] as Record<
      string,
      unknown
    >;
    expect(props).toMatchObject({ control: "replay_slide", via: "double_tap" });
    expect(props.lesson_id).toBeUndefined();
  });
  it("drops values outside the catalogue", () => {
    trackImmersive({
      event: "immersive_control_used",
      control: "free text" as never,
      via: "button",
    });
    trackImmersive({
      event: "immersive_mode_entered",
      course_id: "has spaces and text",
      lesson_id: "l1",
      slide_index: 0,
      trigger: "control",
    });
    expect(posthog.capture).not.toHaveBeenCalled();
  });
  it("sends nothing for an opted-out Learner", () => {
    on(true);
    trackImmersive({
      event: "immersive_control_used",
      control: "play",
      via: "button",
    });
    expect(posthog.capture).not.toHaveBeenCalled();
  });
});
