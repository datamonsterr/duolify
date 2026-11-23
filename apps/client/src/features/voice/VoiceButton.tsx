import { useRef, useState, type PointerEvent, type KeyboardEvent } from "react";
import type { VoiceEntryPoint } from "../../lib/analytics";
import { useUiPreferences } from "../design/localization/Preferences";
import { autoSendEnabled, explicitAutoSend, setAutoSend } from "./autosend";
import {
  useVoiceInput,
  type Transcript,
  type VoiceBlock,
  type VoiceNotice,
} from "./useVoiceInput";
import type { VoicePlatform } from "./platform";
import "./voice.css";

const EXPLAINED = "duolify.voice.explained";
const wasExplained = () => {
  try {
    return localStorage.getItem(EXPLAINED) === "1";
  } catch {
    return false;
  }
};

const clock = (ms: number) => {
  const s = Math.floor(ms / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
};

type T = ReturnType<typeof useUiPreferences>["t"];

function blockText(t: T, block: Exclude<VoiceBlock, undefined>, resets?: Date) {
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

function noticeText(t: T, n: VoiceNotice) {
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

/**
 * Push-to-talk microphone control: hold to record, release to transcribe, slide off or press
 * Escape to cancel, tap to toggle. Typing stays available next to it, always.
 */
export function VoiceButton({
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
  const [auto, setAuto] = useState(() => autoSendEnabled("en"));
  const button = useRef<HTMLButtonElement>(null);
  const recording = voice.phase === "recording";
  const transcribing = voice.phase === "transcribing";
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
        aria-disabled={inactive || transcribing}
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={() => recording && voice.release()}
        onPointerCancel={() => recording && !voice.toggled && voice.slideOff()}
        onContextMenu={(event) => event.preventDefault()}
        onKeyDown={(event) => key(event, true)}
        onKeyUp={(event) => key(event, false)}
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
      {recording && (
        <p className="voice-hint">
          {voice.toggled ? t("voice.hint_toggled") : t("voice.hint_hold")}
        </p>
      )}
      {transcribing && (
        <p className="voice-hint" role="status">
          {t("voice.hint_transcribing")}
        </p>
      )}
      {!recording &&
        voice.block &&
        voice.block !== "denied" &&
        voice.block !== "loading" && (
          <p className="voice-note" role="status" data-testid="voice-blocked">
            {blockText(t, voice.block, voice.capResets)}
          </p>
        )}
      {!recording && !transcribing && voice.notice && (
        <p className="voice-note" role="alert" data-testid="voice-notice">
          {noticeText(t, voice.notice)}
          {voice.notice.kind === "denied" && (
            <button
              type="button"
              className="voice-retry"
              onClick={() => void voice.requestPermission()}
            >
              {t("voice.try_again")}
            </button>
          )}
          {((voice.notice.kind === "empty" && voice.notice.retry) ||
            (voice.notice.kind === "error" && voice.notice.retry)) && (
            <button type="button" className="voice-retry" onClick={voice.retry}>
              {t("voice.try_again")}
            </button>
          )}
          <button type="button" className="voice-retry" onClick={voice.dismiss}>
            {t("voice.type_instead")}
          </button>
        </p>
      )}
      {purpose === "tutor" && !recording && (
        <label className="voice-hint voice-auto">
          <input
            type="checkbox"
            data-testid="voice-auto-send"
            checked={explicitAutoSend() ?? auto}
            onChange={(event) => {
              setAuto(event.target.checked);
              setAutoSend(event.target.checked);
            }}
          />{" "}
          {t("voice.auto_send")}
        </label>
      )}
      {explaining && (
        <div
          className="voice-explain"
          role="dialog"
          aria-label={t("voice.explain_title")}
          data-testid="voice-explain"
        >
          <p>{t("voice.explain_body")}</p>
          <div className="voice-row">
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
            <button
              type="button"
              className="voice-retry"
              data-testid="voice-explain-skip"
              onClick={() => setExplaining(false)}
            >
              {t("voice.not_now")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
