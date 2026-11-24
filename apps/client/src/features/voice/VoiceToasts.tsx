import { useEffect, useState } from "react";
import { showToast, dismissToast } from "../../components/toast";
import { ToastHost } from "../../components/toast/ToastHost";
import { useUiPreferences } from "../design/localization/Preferences";
import { noticeText } from "./voice-text";
import type { useVoiceInput } from "./useVoiceInput";

/** The plain control's hints and notices as toasts; loads on first use, with its own host when the page has none. */
export default function VoiceToasts({
  voice,
  reason,
}: {
  voice: ReturnType<typeof useVoiceInput>;
  reason: string;
}) {
  const { t } = useUiPreferences();
  const recording = voice.phase === "recording";
  const transcribing = voice.phase === "transcribing";
  //
  const [needsHost, setNeedsHost] = useState(false);
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- one DOM probe after mount
    setNeedsHost(!document.querySelector(".ui-toast-host"));
  }, []);
  useEffect(() => {
    if (recording)
      showToast({
        id: "voice-hint",
        message: voice.toggled ? t("voice.hint_toggled") : t("voice.hint_hold"),
        ttlMs: 60_000,
      });
    else if (transcribing)
      showToast({
        id: "voice-hint",
        message: t("voice.hint_transcribing"),
        ttlMs: 60_000,
      });
    else dismissToast("voice-hint");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [recording, transcribing, voice.toggled]);
  useEffect(() => {
    if (!recording && reason)
      showToast({ id: "voice-reason", message: reason });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reason]);
  useEffect(() => {
    const n = voice.notice;
    if (!n || recording || transcribing) return;
    showToast({
      id: "voice-notice",
      message: noticeText(t, n),
      tone: n.kind === "error" || n.kind === "denied" ? "error" : "info",
      action:
        n.kind === "denied"
          ? {
              label: t("voice.try_again"),
              onAction: () => void voice.requestPermission(),
            }
          : (n.kind === "empty" || n.kind === "error") && n.retry
            ? { label: t("voice.try_again"), onAction: voice.retry }
            : undefined,
    });
    // The audio stays kept so the toast's Try again can upload it again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [voice.notice, recording, transcribing]);
  return needsHost ? <ToastHost /> : null;
}
