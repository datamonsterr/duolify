export type FeedbackCueType = "tap" | "completion" | "correction";
export type HapticsSetting = "system" | "on" | "off";

export interface FeedbackCueOptions {
  type: FeedbackCueType;
  soundEnabled?: boolean;
  hapticsSetting?: HapticsSetting;
  reduceMotion?: boolean;
}

export interface FeedbackSettings {
  ui_sounds: boolean;
  haptics: HapticsSetting;
}

let activeFeedbackSettings: FeedbackSettings = {
  ui_sounds: false,
  haptics: "system",
};

/**
 * Reads the current feedback settings from localStorage (if available) or in-memory state.
 */
export function readFeedbackSettings(): FeedbackSettings {
  try {
    if (typeof localStorage !== "undefined") {
      const sounds = localStorage.getItem("duolify.ui-sounds");
      const haptics = localStorage.getItem(
        "duolify.haptics",
      ) as HapticsSetting | null;
      if (sounds !== null || haptics !== null) {
        return {
          ui_sounds: sounds === "true",
          haptics:
            haptics === "on" || haptics === "off" || haptics === "system"
              ? haptics
              : activeFeedbackSettings.haptics,
        };
      }
    }
  } catch {
    /* Safe fallback to in-memory */
  }
  return { ...activeFeedbackSettings };
}

/**
 * Updates the active feedback settings and persists them to localStorage for cross-view access.
 */
export function setFeedbackSettings(settings: Partial<FeedbackSettings>): void {
  activeFeedbackSettings = {
    ...activeFeedbackSettings,
    ...settings,
  };
  try {
    if (typeof localStorage !== "undefined") {
      if (settings.ui_sounds !== undefined) {
        localStorage.setItem("duolify.ui-sounds", String(settings.ui_sounds));
      }
      if (settings.haptics !== undefined) {
        localStorage.setItem("duolify.haptics", settings.haptics);
      }
    }
  } catch {
    /* Safe fallback */
  }
}

/**
 * ADR 0013: The current original placeholder mark, green theme and tokens are approved as the v1 brand.
 * No UI sound cues ship until assets exist; optional feedback settings stay.
 * Returns false so UI cues remain silent no-ops without audio assets or network requests.
 */
export function shouldPlaySound(options?: { soundEnabled?: boolean }): boolean {
  void options;
  return false;
}

/**
 * Determines the haptic vibration pattern for a given interaction cue.
 * - Tap: one light tap ([15ms])
 * - Completion: short paired pulse ([20ms, 40ms, 20ms])
 * - Corrections/errors: never punish mistakes (null)
 * - Off setting disables all haptics (null)
 * - docs/design/brand.md: Reduced motion suppresses celebration movement only;
 *   haptics and sound remain separate opt-in controls. Completion keeps the short
 *   paired pulse when haptics are allowed regardless of reduced motion.
 */
export function determineHapticPattern(options: {
  type: FeedbackCueType;
  hapticsSetting?: HapticsSetting;
  reduceMotion?: boolean;
}): number[] | null {
  const { type, hapticsSetting = "system" } = options;

  if (hapticsSetting === "off") {
    return null;
  }

  // Never punish mistakes
  if (type === "correction") {
    return null;
  }

  if (type === "completion") {
    // Keeps short paired pulse regardless of reduced motion
    return [20, 40, 20];
  }

  if (type === "tap") {
    return [15];
  }

  return null;
}

/**
 * Platform bridge for vibrating the device (Web Vibration API or Tauri bridge).
 */
export function vibratePlatform(pattern: number[]): void {
  try {
    if (
      typeof navigator !== "undefined" &&
      typeof navigator.vibrate === "function"
    ) {
      navigator.vibrate(pattern);
      return;
    }

    if (
      typeof window !== "undefined" &&
      (
        window as unknown as {
          __TAURI_INTERNALS__?: {
            invoke: (cmd: string, args?: unknown) => unknown;
          };
        }
      ).__TAURI_INTERNALS__?.invoke
    ) {
      const internals = (
        window as unknown as {
          __TAURI_INTERNALS__: {
            invoke: (cmd: string, args?: unknown) => unknown;
          };
        }
      ).__TAURI_INTERNALS__;
      internals.invoke("plugin:vibrate|vibrate", { pattern });
    }
  } catch {
    /* Haptics are progressive enhancements and must never crash the UI */
  }
}

/**
 * Executes a feedback cue according to the learner's settings and platform capabilities.
 * Accepts either a FeedbackCueType string or a detailed FeedbackCueOptions object.
 */
export function triggerFeedbackCue(
  optionsOrCue: FeedbackCueOptions | FeedbackCueType,
): void {
  const options: FeedbackCueOptions =
    typeof optionsOrCue === "string" ? { type: optionsOrCue } : optionsOrCue;

  const current = readFeedbackSettings();
  const soundEnabled = options.soundEnabled ?? current.ui_sounds;
  const hapticsSetting = options.hapticsSetting ?? current.haptics;

  // 1. Audio: silent no-op in v1 (no assets exist per ADR 0013)
  if (shouldPlaySound({ soundEnabled })) {
    // No-op
  }

  // 2. Haptics: execute vibration if pattern determined
  const pattern = determineHapticPattern({
    type: options.type,
    hapticsSetting,
    reduceMotion: options.reduceMotion,
  });
  if (pattern && pattern.length > 0) {
    vibratePlatform(pattern);
  }
}

export const FeedbackBridge = {
  shouldPlaySound,
  determineHapticPattern,
  vibratePlatform,
  triggerFeedbackCue,
  readFeedbackSettings,
  setFeedbackSettings,
};
