import {
  profileApi,
  type LearnerSettings,
} from "../../accounts/learner-context";
import { savePlayerPreferences } from "../../player/preferences";

// Per-Learner playback settings live on the settings API and in the local player
// preferences. The local copy is the offline fallback: a change made while the API is
// unreachable stays queued here and is sent with the next successful call.
const PENDING_KEY = "duolify.player-settings-pending";
export type PlaybackSettings = Pick<
  LearnerSettings,
  "autoplay" | "fullscreen_on_rotate" | "auto_send_after_dictation"
>;
const FIELDS = [
  "autoplay",
  "fullscreen_on_rotate",
  "auto_send_after_dictation",
] as const;

function readPending(): Partial<PlaybackSettings> {
  try {
    const raw = JSON.parse(localStorage.getItem(PENDING_KEY) ?? "{}");
    return Object.fromEntries(
      FIELDS.filter((k) => typeof raw[k] === "boolean").map((k) => [k, raw[k]]),
    );
  } catch {
    return {};
  }
}
function writePending(value: Partial<PlaybackSettings>) {
  try {
    if (Object.keys(value).length)
      localStorage.setItem(PENDING_KEY, JSON.stringify(value));
    else localStorage.removeItem(PENDING_KEY);
  } catch {
    /* private mode: the in-memory value still applies this session */
  }
}

export function applyPlaybackSettings(s: PlaybackSettings) {
  savePlayerPreferences({
    autoplay: s.autoplay,
    fullscreenOnRotate: s.fullscreen_on_rotate,
    autoSendAfterDictation: s.auto_send_after_dictation,
  });
}

/** Save a change locally (already done by the caller) and to the API when reachable. */
export async function pushPlaybackSettings(patch: Partial<PlaybackSettings>) {
  const pending = { ...readPending(), ...patch };
  writePending(pending);
  if (typeof navigator !== "undefined" && !navigator.onLine) return;
  try {
    await profileApi.saveSettings(pending);
    writePending({});
  } catch {
    /* stays queued for the next call */
  }
}

/** Send queued changes, then read the stored values into the local preferences. */
export async function refreshPlaybackSettings(): Promise<PlaybackSettings | null> {
  if (typeof navigator !== "undefined" && !navigator.onLine) return null;
  try {
    const pending = readPending();
    if (Object.keys(pending).length) {
      const saved = await profileApi.saveSettings(pending);
      writePending({});
      applyPlaybackSettings(saved);
      return saved;
    }
    const value = await profileApi.readSettings();
    applyPlaybackSettings(value);
    return value;
  } catch {
    return null;
  }
}
