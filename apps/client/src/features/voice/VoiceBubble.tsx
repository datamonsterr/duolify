import {
  useEffect,
  useId,
  useRef,
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent,
} from "react";
import type { VoiceEntryPoint } from "../../lib/analytics";
import { showToast } from "../../components/toast";
import { Icon } from "../design/Icon";
import { useUiPreferences } from "../design/localization/Preferences";
import type { VoicePlatform } from "./platform";
import {
  useVoiceInput,
  type Transcript,
  type VoiceNotice,
} from "./useVoiceInput";
import {
  blockText,
  clock,
  EXPLAINED,
  noticeText,
  wasExplained,
} from "./voice-text";
import "./voice.css";

const isError = (n: VoiceNotice) => n.kind === "error" || n.kind === "denied";

/**
 * Push-to-talk as a small round icon bubble (visual 40 px, 44 px target). Same hold, release,
 * slide-off, tap-to-toggle and Escape behaviour as the text control; the recording state sits on
 * the bubble (level ring, timer badge) and every explanation is a toast.
 */
export function VoiceBubble({
  purpose,
  entryPoint,
  lessonId,
  onTranscript,
  disabled,
  platform,
  disabledReason,
}: {
  purpose: "tutor" | "note";
  entryPoint: VoiceEntryPoint;
  lessonId: string | undefined;
  onTranscript: (transcript: Transcript) => void;
  disabled: boolean;
  platform?: VoicePlatform;
  disabledReason?: string;
}) {
  const voice = useVoiceInput({
    purpose,
    entryPoint,
    lessonId,
    onTranscript,
    platform,
  });
  const { t } = useUiPreferences();
  const reasonId = useId();
  const recording = voice.phase === "recording";
  const transcribing = voice.phase === "transcribing";
  const blocked = voice.block !== undefined && voice.block !== "denied";
  const blockMessage =
    voice.block && voice.block !== "loading" && voice.block !== "no_lesson"
      ? blockText(t, voice.block, voice.capResets)
      : "";
  const reason =
    (blocked ? blockMessage : "") || (disabled ? (disabledReason ?? "") : "");
  const inactive = disabled || blocked;

  const retryAction = (n: VoiceNotice) =>
    n.kind === "denied"
      ? {
          label: t("voice.try_again"),
          onAction: () => void voice.requestPermission(),
        }
      : (n.kind === "empty" || n.kind === "error") && n.retry
        ? { label: t("voice.try_again"), onAction: voice.retry }
        : undefined;
  useEffect(() => {
    const n = voice.notice;
    if (!n) return;
    showToast({
      id: "voice-notice",
      message: noticeText(t, n),
      tone: isError(n) ? "error" : "info",
      action: retryAction(n),
    });
    voice.dismiss();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [voice.notice]);
  // A block that appears while the Learner is here (went offline, cap reached) is announced at once.
  const lastBlock = useRef(voice.block);
  useEffect(() => {
    const was = lastBlock.current;
    lastBlock.current = voice.block;
    if (was === voice.block || !blockMessage || voice.block === "denied")
      return;
    showToast({ id: "voice-reason", message: blockMessage, tone: "info" });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [voice.block]);
  useEffect(() => {
    if (voice.cancelled)
      showToast({ id: "voice-cancelled", message: t("voice.cancelled") });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [voice.cancelled]);

  const explainFirst = () => {
    showToast({
      id: "voice-explain",
      message: t("voice.explain_short"),
      ttlMs: 8000,
      action: {
        label: t("voice.allow"),
        onAction: async () => {
          try {
            localStorage.setItem(EXPLAINED, "1");
          } catch {
            /* asked again next time */
          }
          await voice.requestPermission();
        },
      },
    });
  };
  const down = (event: PointerEvent<HTMLButtonElement>) => {
    if (inactive) {
      if (reason) showToast({ id: "voice-reason", message: reason });
      return;
    }
    if (voice.block === "denied") {
      showToast({
        id: "voice-notice",
        message: t("voice.denied_block"),
        tone: "error",
        action: {
          label: t("voice.try_again"),
          onAction: () => void voice.requestPermission(),
        },
      });
      return;
    }
    if (!wasExplained()) return explainFirst();
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
      if (!wasExplained()) explainFirst();
      else voice.press();
    } else voice.release();
  };
  if (voice.block === "no_lesson") return null;

  const label = recording
    ? voice.toggled
      ? t("voice.stop_label")
      : t("voice.recording_label")
    : transcribing
      ? t("voice.transcribing")
      : t("voice.hold_label");
  return (
    <div className="voice-bubble-wrap" data-testid="voice-ptt">
      <button
        type="button"
        className={`voice-bubble${recording ? " recording" : ""}`}
        data-testid="voice-mic"
        data-purpose={purpose}
        style={{ "--voice-level": voice.level } as CSSProperties}
        aria-label={label}
        title={reason || label}
        aria-pressed={recording}
        aria-busy={transcribing}
        aria-disabled={inactive || transcribing}
        aria-describedby={reason ? reasonId : undefined}
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={() => recording && voice.release()}
        onPointerCancel={() => recording && !voice.toggled && voice.slideOff()}
        onContextMenu={(event) => event.preventDefault()}
        onKeyDown={(event) => key(event, true)}
        onKeyUp={(event) => key(event, false)}
      >
        <Icon name="microphone" />
        {recording && (
          <span className="voice-badge" data-testid="voice-timer" role="timer">
            {clock(voice.elapsedMs)}
          </span>
        )}
      </button>
      {recording && (
        <span
          className="voice-ring"
          role="meter"
          aria-label={t("voice.level")}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(voice.level * 100)}
        />
      )}
      {reason && (
        <span id={reasonId} className="voice-reason">
          {reason}
        </span>
      )}
    </div>
  );
}
