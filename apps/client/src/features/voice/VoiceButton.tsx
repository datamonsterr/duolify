import { ActionFooter } from "../design/sheet/ActionFooter";
import {
  lazy,
  Suspense,
  useId,
  useRef,
  useState,
  type PointerEvent,
  type KeyboardEvent,
} from "react";
import type { VoiceEntryPoint } from "../../lib/analytics";
import { useUiPreferences } from "../design/localization/Preferences";
import { useVoiceInput, type Transcript } from "./useVoiceInput";
import type { VoicePlatform } from "./platform";
import { blockText, clock, EXPLAINED, wasExplained } from "./voice-text";
import "./voice.css";

/**
 * Push-to-talk microphone control: hold to record, release to transcribe, slide off or press
 * Escape to cancel, tap to toggle. Typing stays available next to it, always.
 */
// The bubble (and its toasts) load with the immersive presentation, not with the first screen.
const VoiceBubble = lazy(() =>
  import("./VoiceBubble").then((m) => ({ default: m.VoiceBubble })),
);

// Only mounted when no toast host is on the page (tutor sheet, notes page).
const VoiceToasts = lazy(() => import("./VoiceToasts"));

function VoiceTextButton({
  purpose,
  entryPoint,
  lessonId,
  onTranscript,
  disabled = false,
  platform,
}: {
  purpose: "tutor" | "note";
  entryPoint: VoiceEntryPoint;
  lessonId: string | undefined;
  onTranscript: (transcript: Transcript) => void;
  disabled?: boolean;
  platform?: VoicePlatform;
}) {
  const voice = useVoiceInput({
    purpose,
    entryPoint,
    lessonId,
    onTranscript,
    platform,
  });
  const [explaining, setExplaining] = useState(false);
  const { t } = useUiPreferences();
  const button = useRef<HTMLButtonElement>(null);
  const recording = voice.phase === "recording";
  const transcribing = voice.phase === "transcribing";
  const reasonId = useId();
  const blockMessage =
    voice.block && voice.block !== "loading" && voice.block !== "no_lesson"
      ? blockText(t, voice.block, voice.capResets)
      : "";
  const reason = voice.block === "denied" ? "" : blockMessage;
  if (voice.block === "no_lesson") return null;
  const inactive =
    disabled || (voice.block !== undefined && voice.block !== "denied");

  const down = (event: PointerEvent<HTMLButtonElement>) => {
    if (inactive || voice.block === "denied") return;
    if (!wasExplained()) {
      setExplaining(true);
      return;
    }
    event.currentTarget.setPointerCapture?.(event.pointerId);
    voice.press();
  };
  const move = (event: PointerEvent<HTMLButtonElement>) => {
    if (!recording || voice.toggled) return;
    const box = event.currentTarget.getBoundingClientRect();
    const slack = 12;
    if (
      event.clientX < box.left - slack ||
      event.clientX > box.right + slack ||
      event.clientY < box.top - slack ||
      event.clientY > box.bottom + slack
    )
      voice.slideOff();
  };
  const key = (event: KeyboardEvent<HTMLButtonElement>, isDown: boolean) => {
    if (event.key !== " " && event.key !== "Enter") return;
    event.preventDefault();
    if (event.repeat || inactive) return;
    if (isDown) {
      if (!wasExplained()) setExplaining(true);
      else voice.press();
    } else voice.release();
  };

  return (
    <div className="voice-ptt" data-testid="voice-ptt">
      <button
        ref={button}
        type="button"
        className={`voice-mic${recording ? " recording" : ""}`}
        data-testid="voice-mic"
        aria-label={
          recording
            ? voice.toggled
              ? t("voice.stop_label")
              : t("voice.recording_label")
            : t("voice.hold_label")
        }
        aria-pressed={recording}
        aria-describedby={reason ? reasonId : undefined}
        aria-disabled={inactive || transcribing}
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={() => recording && voice.release()}
        onPointerCancel={() => recording && !voice.toggled && voice.slideOff()}
        onContextMenu={(event) => event.preventDefault()}
        onKeyDown={(event) => key(event, true)}
        onKeyUp={(event) => key(event, false)}
        onBlur={() => recording && !voice.toggled && voice.slideOff()}
      >
        <span aria-hidden="true">{recording ? "●" : "🎤"}</span>
        <span>
          {transcribing
            ? t("voice.transcribing")
            : recording
              ? t("voice.recording")
              : t("voice.talk")}
        </span>
      </button>
      {recording && (
        <div className="voice-live" data-testid="voice-live" role="status">
          <span className="voice-dot" aria-hidden="true" />
          <span data-testid="voice-timer">{clock(voice.elapsedMs)}</span>
          <span
            className="voice-meter"
            role="meter"
            aria-label={t("voice.level")}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(voice.level * 100)}
          >
            <span style={{ width: `${Math.round(voice.level * 100)}%` }} />
          </span>
        </div>
      )}
      <Suspense fallback={null}>
        <VoiceToasts voice={voice} reason={reason} />
      </Suspense>
      {reason && (
        <span id={reasonId} className="imm-sr">
          {reason}
        </span>
      )}
      {explaining && (
        <div
          className="voice-explain"
          role="dialog"
          aria-label={t("voice.explain_title")}
          data-testid="voice-explain"
        >
          <p>{t("voice.explain_body")}</p>
          <ActionFooter className="voice-row">
            <button
              type="button"
              className="voice-retry"
              data-testid="voice-explain-skip"
              onClick={() => setExplaining(false)}
            >
              {t("voice.not_now")}
            </button>
            <button
              type="button"
              className="voice-retry"
              data-testid="voice-explain-allow"
              onClick={async () => {
                try {
                  localStorage.setItem(EXPLAINED, "1");
                } catch {
                  /* asked again next time */
                }
                setExplaining(false);
                await voice.requestPermission();
              }}
            >
              {t("voice.allow")}
            </button>
          </ActionFooter>
        </div>
      )}
    </div>
  );
}

export function VoiceButton(
  props: Parameters<typeof VoiceTextButton>[0] & {
    /** "bubble": a small round icon control; every explanation is a toast. */
    variant?: "default" | "bubble";
    /** Why the parent disabled the control; shown as a toast and as its description. */
    disabledReason?: string;
  },
) {
  const { variant, disabledReason, disabled = false, ...rest } = props;
  return variant === "bubble" ? (
    <Suspense fallback={null}>
      <VoiceBubble
        {...rest}
        disabled={disabled}
        disabledReason={disabledReason}
      />
    </Suspense>
  ) : (
    <VoiceTextButton {...rest} disabled={disabled} />
  );
}
