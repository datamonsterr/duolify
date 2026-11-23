import { useRef, useState, type PointerEvent, type KeyboardEvent } from "react";
import type { VoiceEntryPoint } from "../../lib/analytics";
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

function blockText(block: Exclude<VoiceBlock, undefined>, resets?: Date) {
  switch (block) {
    case "offline":
      return "Voice needs a connection. You can still type.";
    case "unavailable":
      return "Voice is not available yet. You can still type.";
    case "unsupported":
      return "This device cannot record audio. You can still type.";
    case "daily_cap":
      return `You have used today's voice limit${
        resets
          ? `; it resets at ${resets.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
          : ""
      }. You can still type.`;
    case "denied":
      return "Microphone access is off. Turn it on for Duolify in your device or browser settings, then try again. You can still type.";
    case "no_lesson":
    case "loading":
      return "";
  }
}

function noticeText(n: VoiceNotice) {
  switch (n.kind) {
    case "denied":
      return "Microphone access is off. Turn it on for Duolify in your device or browser settings, then try again.";
    case "no_microphone":
      return "No microphone was found. You can still type.";
    case "silent":
      return "I could not hear anything. Try again closer to the microphone.";
    case "too_short":
      return "Hold a little longer to record.";
    case "empty":
      return "I could not make out any words.";
    case "error":
      return n.cls === "rate_limited"
        ? "Too many recordings in a row. Wait a moment, then try again."
        : n.cls === "too_large" || n.cls === "too_long"
          ? "That recording was too long to send. Keep it under 30 seconds."
          : n.cls === "unavailable"
            ? "Voice is not available right now. You can still type."
            : n.cls === "offline"
              ? "Voice needs a connection. Try again when you are back online."
              : "I could not turn that into text. You can retry or type.";
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
              ? "Stop recording"
              : "Recording. Release to send, slide away to cancel"
            : "Hold to talk"
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
          {transcribing ? "Transcribing…" : recording ? "Recording" : "Talk"}
        </span>
      </button>
      {recording && (
        <div className="voice-live" data-testid="voice-live" role="status">
          <span className="voice-dot" aria-hidden="true" />
          <span data-testid="voice-timer">{clock(voice.elapsedMs)}</span>
          <span
            className="voice-meter"
            role="meter"
            aria-label="Microphone level"
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
          {voice.toggled
            ? "Tap the microphone again to finish. Press Escape to cancel."
            : "Release to send. Slide away or press Escape to cancel."}
        </p>
      )}
      {transcribing && (
        <p className="voice-hint" role="status">
          Turning your voice into text…
        </p>
      )}
      {!recording &&
        voice.block &&
        voice.block !== "denied" &&
        voice.block !== "loading" && (
          <p className="voice-note" role="status" data-testid="voice-blocked">
            {blockText(voice.block, voice.capResets)}
          </p>
        )}
      {!recording && !transcribing && voice.notice && (
        <p className="voice-note" role="alert" data-testid="voice-notice">
          {noticeText(voice.notice)}
          {voice.notice.kind === "denied" && (
            <button
              type="button"
              className="voice-retry"
              onClick={() => void voice.requestPermission()}
            >
              Try again
            </button>
          )}
          {((voice.notice.kind === "empty" && voice.notice.retry) ||
            (voice.notice.kind === "error" && voice.notice.retry)) && (
            <button type="button" className="voice-retry" onClick={voice.retry}>
              Try again
            </button>
          )}
          <button type="button" className="voice-retry" onClick={voice.dismiss}>
            Type instead
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
          Send my question automatically after 2 seconds
        </label>
      )}
      {explaining && (
        <div
          className="voice-explain"
          role="dialog"
          aria-label="Use the microphone"
          data-testid="voice-explain"
        >
          <p>
            Dictation uses your microphone only while you hold the button. The
            recording is sent to a speech provider that does not keep it (zero
            data retention) and is turned into text. Duolify does not save the
            audio; only the text you send is kept.
          </p>
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
              Allow microphone
            </button>
            <button
              type="button"
              className="voice-retry"
              data-testid="voice-explain-skip"
              onClick={() => setExplaining(false)}
            >
              Not now
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
