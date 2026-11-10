import posthog from "posthog-js/dist/module.no-external";

const uuidRegex =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

const renderedVisits = new Set<string>();

export function resetCharacterRenderDeduplication(): void {
  renderedVisits.clear();
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
