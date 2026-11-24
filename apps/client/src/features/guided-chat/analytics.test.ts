import { beforeEach, expect, it, vi } from "vitest";
import { configureAnalytics, trackGuidedChatEvent } from "../../lib/analytics";
import { posthog } from "../../lib/posthog";

vi.mock("../../lib/posthog", () => ({
  posthog: {
    capture: vi.fn(),
    identify: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_out_capturing: vi.fn(),
    opt_in_capturing: vi.fn(),
  },
}));
const learnerId = "11111111-1111-4111-8111-111111111111";
beforeEach(() => {
  vi.clearAllMocks();
  configureAnalytics({ learnerId, optedOut: false, platform: "web" });
});

it.each([
  "guided_chat_started",
  "guided_chat_resumed",
  "guided_chat_step_answered",
  "guided_chat_step_edited",
  "guided_chat_free_text_asked",
  "guided_chat_abandoned",
  "guided_chat_completed",
] as const)(
  "%s carries ids and enums only, with entry_point not a source",
  (event) => {
    trackGuidedChatEvent({
      event,
      scriptId: "account.delete",
      entryPoint: "settings",
      stepId: "confirm",
      widgetType: "chips+text",
      durationMs: 12_345,
      freeTextCount: 3,
    });
    const [name, props] = vi.mocked(posthog.capture).mock.calls[0];
    expect(name).toBe(event);
    expect(props).toMatchObject({
      distinct_id: learnerId,
      source: "client",
      script_id: "account.delete",
      entry_point: "settings",
      step_id: "confirm",
      widget_type: "chips_text",
      duration_ms: 60_000,
      free_text_count: 3,
    });
    expect(Object.keys(props as object).sort()).not.toContain("answer");
  },
);

it("drops events with free-form ids and does nothing when opted out", () => {
  trackGuidedChatEvent({
    event: "guided_chat_started",
    scriptId: "My Answer, with spaces",
    entryPoint: "settings",
  });
  expect(posthog.capture).not.toHaveBeenCalled();
  configureAnalytics({ learnerId, optedOut: true, platform: "web" });
  trackGuidedChatEvent({
    event: "guided_chat_started",
    scriptId: "account.delete",
    entryPoint: "settings",
  });
  expect(posthog.capture).not.toHaveBeenCalled();
});
