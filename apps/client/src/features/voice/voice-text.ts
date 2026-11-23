import type { useUiPreferences } from "../design/localization/Preferences";
import type { VoiceBlock, VoiceNotice } from "./useVoiceInput";

export const EXPLAINED = "duolify.voice.explained";
export const wasExplained = () => {
  try {
    return localStorage.getItem(EXPLAINED) === "1";
  } catch {
    return false;
  }
};

export const clock = (ms: number) => {
  const s = Math.floor(ms / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
};

export type T = ReturnType<typeof useUiPreferences>["t"];

export function blockText(
  t: T,
  block: Exclude<VoiceBlock, undefined>,
  resets?: Date,
) {
  switch (block) {
    case "offline":
      return t("voice.offline");
    case "unavailable":
      return t("voice.unavailable");
    case "unsupported":
      return t("voice.unsupported");
    case "daily_cap":
      return resets
        ? t("voice.cap_resets", {
            time: resets.toLocaleTimeString([], {
              hour: "2-digit",
              minute: "2-digit",
            }),
          })
        : t("voice.cap");
    case "denied":
      return t("voice.denied_block");
    case "no_lesson":
    case "loading":
      return "";
  }
}

export function noticeText(t: T, n: VoiceNotice) {
  switch (n.kind) {
    case "denied":
      return t("voice.denied");
    case "no_microphone":
      return t("voice.no_microphone");
    case "silent":
      return t("voice.silent");
    case "too_short":
      return t("voice.too_short");
    case "empty":
      return t("voice.empty");
    case "error":
      return n.cls === "rate_limited"
        ? t("voice.rate_limited")
        : n.cls === "too_large" || n.cls === "too_long"
          ? t("voice.too_long")
          : n.cls === "unavailable"
            ? t("voice.unavailable_now")
            : n.cls === "offline"
              ? t("voice.offline_retry")
              : t("voice.failed");
  }
}
