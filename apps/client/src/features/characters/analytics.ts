import posthog from "posthog-js/dist/module.no-external";

const uuidRegex =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

const renderedVisits = new Set<string>();
const reactionResults = new Set<string>();

export function resetCharacterRenderDeduplication(): void {
  renderedVisits.clear();
  reactionResults.clear();
}

export interface CharacterRenderedParams {
  speaker_key: string;
  character_version: number;
  rig_version: string;
  expression: string;
  offline: boolean;
  slide_id?: string;
  visit_id?: string;
}

export function trackCharacterRendered(params: CharacterRenderedParams): void {
  const visitKey = `${params.slide_id ?? "default"}:${params.speaker_key}:${params.visit_id ?? "1"}`;
  if (renderedVisits.has(visitKey)) {
    return;
  }

  try {
    if (posthog.has_opted_out_capturing && posthog.has_opted_out_capturing()) {
      return;
    }

    let distinctId = "00000000-0000-0000-0000-000000000001";
    const platform = "web";
    const appVersion = "0.1.0";

    try {
      const stored = sessionStorage.getItem("duolify.auth");
      if (stored) {
        const parsed = JSON.parse(stored);
        if (parsed.learnerId && uuidRegex.test(parsed.learnerId)) {
          distinctId = parsed.learnerId;
        }
      }
    } catch {
      /* Storage unavailable */
    }

    posthog.capture("character_rendered", {
      distinct_id: distinctId,
      source: "client",
      platform,
      app_version: appVersion,
      speaker_key: params.speaker_key,
      character_version: params.character_version,
      rig_version: params.rig_version,
      expression: params.expression,
      offline: params.offline,
    });

    renderedVisits.add(visitKey);
  } catch {
    /* Telemetry must never crash client */
  }
}

export interface CharacterRenderFallbackParams {
  reason_code: string;
  offline: boolean;
}

export function trackCharacterRenderFallback(
  params: CharacterRenderFallbackParams,
): void {
  try {
    if (posthog.has_opted_out_capturing && posthog.has_opted_out_capturing()) {
      return;
    }

    let distinctId = "00000000-0000-0000-0000-000000000001";
    const platform = "web";
    const appVersion = "0.1.0";

    try {
      const stored = sessionStorage.getItem("duolify.auth");
      if (stored) {
        const parsed = JSON.parse(stored);
        if (parsed.learnerId && uuidRegex.test(parsed.learnerId)) {
          distinctId = parsed.learnerId;
        }
      }
    } catch {
      /* Storage unavailable */
    }

    posthog.capture("character_render_fallback", {
      distinct_id: distinctId,
      source: "client",
      platform,
      app_version: appVersion,
      reason_code: params.reason_code,
      offline: params.offline,
    });
  } catch {
    /* Telemetry must never crash client */
  }
}

export interface CharacterReactionShownParams {
  reaction_trigger: "answer_correct" | "answer_incorrect" | "test_passed";
  speaker_key: string;
  expression: string;
  fallback_used: boolean;
  /** Operation/result identity; one event per result. Never sent. */
  result_key: string;
}

/** Content-free: only closed enums and flags leave the device. */
export function trackCharacterReactionShown(
  params: CharacterReactionShownParams,
): void {
  if (reactionResults.has(params.result_key)) return;
  reactionResults.add(params.result_key);
  try {
    if (posthog.has_opted_out_capturing && posthog.has_opted_out_capturing()) {
      return;
    }
    let distinctId = "00000000-0000-0000-0000-000000000001";
    try {
      const stored = sessionStorage.getItem("duolify.auth");
      if (stored) {
        const parsed = JSON.parse(stored);
        if (parsed.learnerId && uuidRegex.test(parsed.learnerId)) {
          distinctId = parsed.learnerId;
        }
      }
    } catch {
      /* Storage unavailable */
    }
    posthog.capture("character_reaction_shown", {
      distinct_id: distinctId,
      source: "client",
      platform: "web",
      app_version: "0.1.0",
      reaction_trigger: params.reaction_trigger,
      speaker_key: params.speaker_key,
      expression: params.expression,
      fallback_used: params.fallback_used,
    });
  } catch {
    /* Telemetry must never crash client */
  }
}
