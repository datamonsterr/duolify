import { useCallback, useEffect, useRef, useState } from "react";
import { trackVoice, type VoiceEntryPoint } from "../../lib/analytics";
import { autoSendEnabled } from "./autosend";
import { EDIT_WINDOW_MS } from "./recorder";

type Active = { text: string; edited: boolean; counting: boolean };

/**
 * The review step of a dictated question: the transcript sits in the composer, and unless the
 * Learner edits or cancels within the window it is sent automatically. Editing stops the
 * automatic send for good. Emits `voice_transcript_used` / `voice_transcript_discarded`.
 */
export function useDictationWindow({
  entryPoint,
  draft,
  send,
}: {
  entryPoint: VoiceEntryPoint;
  draft: string;
  /** The composer's own send; must read the current draft. Return false when nothing was sent. */
  send: () => boolean | void;
}) {
  const [active, setActive] = useState<Active | null>(null);
  const activeRef = useRef<Active | null>(null);
  const sendRef = useRef(send);
  useEffect(() => {
    sendRef.current = send;
    activeRef.current = active;
  });
  const emit = useCallback(
    (
      event: "voice_transcript_used" | "voice_transcript_discarded",
      edited: boolean,
    ) =>
      trackVoice({ event, purpose: "tutor", entry_point: entryPoint, edited }),
    [entryPoint],
  );

  const begin = useCallback((text: string, language: string) => {
    setActive({ text, edited: false, counting: autoSendEnabled(language) });
  }, []);

  /** Call from the composer's send path (manual or automatic). */
  const noteSent = useCallback(() => {
    const current = activeRef.current;
    if (!current) return;
    activeRef.current = null;
    setActive(null);
    emit("voice_transcript_used", current.edited);
  }, [emit]);

  const discard = useCallback(() => {
    const current = activeRef.current;
    if (!current) return;
    activeRef.current = null;
    setActive(null);
    emit("voice_transcript_discarded", current.edited);
  }, [emit]);

  // An edit stops the automatic send; clearing the field throws the transcript away.
  /* eslint-disable react-hooks/set-state-in-effect -- an edit must stop the countdown in the same commit */
  useEffect(() => {
    if (!active || draft === active.text) return;
    if (draft.trim() === "") {
      activeRef.current = null;
      setActive(null);
      emit("voice_transcript_discarded", true);
    } else if (!active.edited)
      setActive({ ...active, edited: true, counting: false });
  }, [draft, active, emit]);
  /* eslint-enable react-hooks/set-state-in-effect */

  useEffect(() => {
    if (!active?.counting) return;
    const id = setTimeout(() => {
      if (!activeRef.current?.counting) return;
      if (sendRef.current() === false) {
        // Consent, connection or a running turn blocked it: the text stays for a manual send.
        setActive((a) => (a ? { ...a, counting: false } : a));
        return;
      }
      noteSent();
    }, EDIT_WINDOW_MS);
    return () => clearTimeout(id);
  }, [active?.counting, active?.text, noteSent]);

  // Leaving with the transcript unsent counts as discarding it.
  useEffect(
    () => () => {
      const current = activeRef.current;
      if (current)
        trackVoice({
          event: "voice_transcript_discarded",
          purpose: "tutor",
          entry_point: entryPoint,
          edited: current.edited,
        });
    },
    [entryPoint],
  );

  return {
    begin,
    noteSent,
    discard,
    /** Stop the countdown without discarding the text (the Cancel control). */
    cancelAuto: () =>
      setActive((a) => (a ? { ...a, counting: false, edited: a.edited } : a)),
    counting: !!active?.counting,
  };
}
