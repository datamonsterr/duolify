import {
  trackGuidedChatEvent,
  type GuidedChatAnalytics,
} from "../../lib/analytics";
import type { GuidedEvent } from "./GuidedScriptChat";
import type { GuidedScript } from "./types";

const names = {
  started: "guided_chat_started",
  resumed: "guided_chat_resumed",
  completed: "guided_chat_completed",
  abandoned: "guided_chat_abandoned",
  step_answered: "guided_chat_step_answered",
  step_edited: "guided_chat_step_edited",
  free_text_asked: "guided_chat_free_text_asked",
} as const;

/** Maps chat events to the content-free PostHog events. Step ids come from the script. */
export function trackGuidedChat(
  script: GuidedScript,
  entryPoint: string,
  event: GuidedEvent,
) {
  const base = { scriptId: script.id, entryPoint };
  if (event.name === "free_text_asked")
    return trackGuidedChatEvent({
      ...base,
      event: names.free_text_asked,
      stepId: event.stepId,
      route: event.route as GuidedChatAnalytics["route"],
      charged: false,
    });
  if (event.name === "step_answered" || event.name === "step_edited") {
    const step = script.steps.find((s) => s.id === event.stepId);
    return trackGuidedChatEvent({
      ...base,
      event: names[event.name],
      stepId: step?.analyticsId ?? event.stepId,
      widgetType: event.widget,
      skipped: event.skipped,
    });
  }
  trackGuidedChatEvent({ ...base, event: names[event.name] });
}
