/** Shown while a dictated question waits out the edit window; editing the text also stops it. */
export function DictationCountdown({
  counting,
  onCancel,
}: {
  counting: boolean;
  onCancel: () => void;
}) {
  if (!counting) return null;
  return (
    <p className="voice-note" role="status" data-testid="voice-countdown">
      Sending in 2 seconds. Edit the text to keep it here.
      <button
        type="button"
        className="voice-retry"
        data-testid="voice-countdown-cancel"
        onClick={onCancel}
      >
        Cancel
      </button>
    </p>
  );
}
