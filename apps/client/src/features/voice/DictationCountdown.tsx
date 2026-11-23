import { useUiPreferences } from "../design/localization/Preferences";

/** Shown while a dictated question waits out the edit window; editing the text also stops it. */
export function DictationCountdown({
  counting,
  onCancel,
}: {
  counting: boolean;
  onCancel: () => void;
}) {
  const { t } = useUiPreferences();
  if (!counting) return null;
  return (
    <p className="voice-note" role="status" data-testid="voice-countdown">
      {t("voice.countdown")}
      <button
        type="button"
        className="voice-retry"
        data-testid="voice-countdown-cancel"
        onClick={onCancel}
      >
        {t("voice.cancel")}
      </button>
    </p>
  );
}
