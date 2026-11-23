import { loadPlayerPreferences } from "../player/preferences";
import { pushPlaybackSettings } from "../lesson-player/immersive/settings";

const KEY = "duolify.voice.auto-send";

/** The Learner's explicit choice, if they ever made one. */
export function explicitAutoSend(): boolean | undefined {
  try {
    const v = localStorage.getItem(KEY);
    return v === "on" ? true : v === "off" ? false : undefined;
  } catch {
    return undefined;
  }
}

/**
 * Whether a dictated question is sent by itself after the edit window. On by default; off by
 * default for Vietnamese until the Golden set measures its word error rate; the Learner's own
 * choice always wins.
 */
export function autoSendEnabled(language: string): boolean {
  const explicit = explicitAutoSend();
  if (explicit !== undefined) return explicit;
  if (!loadPlayerPreferences().autoSendAfterDictation) return false;
  return !/^vi(\b|-|_)/i.test(language.trim());
}

export function setAutoSend(on: boolean) {
  try {
    localStorage.setItem(KEY, on ? "on" : "off");
  } catch {
    /* the in-memory choice below still applies */
  }
  void pushPlaybackSettings({ auto_send_after_dictation: on });
}
