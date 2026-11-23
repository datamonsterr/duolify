import { useUiPreferences } from "../design/localization/Preferences";

/** A small chip while a dictated question waits out the edit window; editing the text also stops it. */
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
    <p className="voice-chip" role="status" data-testid="voice-countdown">
      <span>{t("voice.countdown")}</span>
      <button
        type="button"
        className="voice-chip-cancel"
        data-testid="voice-countdown-cancel"
        aria-label={t("voice.cancel")}
        title={t("voice.cancel")}
        onClick={onCancel}
      >
        <span aria-hidden="true">×</span>
      </button>
    </p>
  );
}
