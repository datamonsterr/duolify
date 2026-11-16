export type FeedbackCueType = "tap" | "completion" | "correction";
export type HapticsSetting = "system" | "on" | "off";

export interface FeedbackCueOptions {
  type: FeedbackCueType;
  soundEnabled?: boolean;
  hapticsSetting?: HapticsSetting;
  reduceMotion?: boolean;
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
 * - Reduced motion suppresses celebratory haptics (completion -> null)
 * - Off setting disables all haptics (null)
 */
export function determineHapticPattern(options: {
  type: FeedbackCueType;
  hapticsSetting?: HapticsSetting;
  reduceMotion?: boolean;
}): number[] | null {
  const { type, hapticsSetting = "system", reduceMotion = false } = options;

  if (hapticsSetting === "off") {
    return null;
  }

  // Never punish mistakes
  if (type === "correction") {
    return null;
  }

  // Check if OS or user prefers reduced motion
  const osPrefersReducedMotion =
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  const isReducedMotion = reduceMotion || osPrefersReducedMotion;

  // Reduced motion suppresses celebration movement and vibration
  if (type === "completion") {
    if (isReducedMotion) {
      return null;
    }
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
 */
export function triggerFeedbackCue(options: FeedbackCueOptions): void {
  // 1. Audio: silent no-op in v1 (no assets exist per ADR 0013)
  if (shouldPlaySound({ soundEnabled: options.soundEnabled })) {
    // No-op
  }

  // 2. Haptics: execute vibration if pattern determined
  const pattern = determineHapticPattern(options);
  if (pattern && pattern.length > 0) {
    vibratePlatform(pattern);
  }
}

export const FeedbackBridge = {
  shouldPlaySound,
  determineHapticPattern,
  vibratePlatform,
  triggerFeedbackCue,
};
