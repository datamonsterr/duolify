import { AuthError, session } from "../accounts/session";

/** Narration voice choice (E14-06). Voices, prices and rights all come from the API. */
export type VoiceInfo = {
  key: string;
  label: string;
  curated: boolean;
  available: boolean;
};
export type VoiceChange = {
  voice_key: string;
  state: "none" | "pending" | "ready" | "failed" | "disabled";
  reason?: string;
};
export type VoiceList = {
  narration_enabled: boolean;
  selected: string;
  entitled: boolean;
  upsell_allowed: boolean;
  config_hash: string;
  voices: VoiceInfo[];
  change?: VoiceChange;
};
export type VoiceQuote = {
  lesson_id: string;
  voice_key: string;
  quote_id?: string;
  characters: number;
  credits: number;
  free: boolean;
  pricing_version?: string;
  config_hash: string;
  expires_at?: string;
};
/** Fixed machine codes the API answers with; never user text. */
export type VoiceErrorCode =
  | "voice_not_entitled"
  | "voice_unavailable"
  | "narration_off"
  | "quote_changed"
  | "unavailable";

export function voiceErrorCode(error: unknown): {
  code: VoiceErrorCode;
  upsellAllowed: boolean;
} {
  if (error instanceof AuthError) {
    const d = (error.details ?? {}) as {
      code?: VoiceErrorCode;
      upsell_allowed?: boolean;
    };
    return { code: d.code ?? "unavailable", upsellAllowed: !!d.upsell_allowed };
  }
  return { code: "unavailable", upsellAllowed: false };
}

const lessonPath = (lessonId: string) =>
  `/v1/lessons/${encodeURIComponent(lessonId)}/narration/voice`;

export const voiceAPI = {
  list: (lessonId: string) => session.request<VoiceList>(lessonPath(lessonId)),
  quote: (lessonId: string, voiceKey: string) =>
    session.request<VoiceQuote>(`${lessonPath(lessonId)}/quote`, "POST", {
      voice_key: voiceKey,
    }),
  apply: (
    lessonId: string,
    voiceKey: string,
    quoteId: string | undefined,
    configHash: string,
  ) =>
    session.request<{ state: "queued" | "ready"; voice_key: string }>(
      lessonPath(lessonId),
      "POST",
      { voice_key: voiceKey, quote_id: quoteId ?? "", config_hash: configHash },
    ),
};
