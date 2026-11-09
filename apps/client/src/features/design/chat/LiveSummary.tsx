import type { MessageId } from "../localization/messages";
import { useUiPreferences } from "../localization/Preferences";
import { trackGuidedUi } from "../../../lib/analytics";
import { Button } from "../Button";
import { Field, Feedback } from "./Feedback";

export type SummaryLine = {
  id: string;
  label: string;
  labelId?: MessageId;
  value: string;
  error?: string;
  required?: boolean;
};
export function LiveSummary({
  lines,
  onChange,
  onConfirm,
  unsaved,
  pending = false,
  confirmed = false,
  error,
}: {
  lines: SummaryLine[];
  onChange: (id: string, value: string) => void;
  onConfirm: () => void;
  unsaved: boolean;
  pending?: boolean;
  confirmed?: boolean;
  error?: string;
}) {
  const { t } = useUiPreferences();
  return (
    <form
      className="guided-summary"
      onSubmit={(event) => {
        event.preventDefault();
        if (!pending) {
          trackGuidedUi({
            event: "ui_control_activated",
            surface: "summary",
            controlId: "confirm",
          });
          onConfirm();
        }
      }}
      noValidate
    >
      <p>{t("summary.intro")}</p>
      {lines.map((line) => (
        <Field
          key={line.id}
          label={line.labelId ? t(line.labelId) : line.label}
          value={line.value}
          required={line.required}
          error={line.error}
          hint={!line.value ? t("summary.wait") : undefined}
          onChange={(event) => {
            trackGuidedUi({
              event: "ui_control_activated",
              surface: "summary",
              controlId: "edit",
            });
            onChange(line.id, event.target.value);
          }}
        />
      ))}
      <Feedback
        title={
          pending
            ? t("summary.pending")
            : confirmed
              ? t("summary.confirmed")
              : unsaved
                ? t("summary.unsaved")
                : t("summary.review")
        }
        tone={confirmed ? "success" : "info"}
      />
      {error && <Feedback tone="danger" title={error} blocking />}
      <p>{t("summary.disclosure")}</p>
      <Button type="submit" pending={pending}>
        {t("summary.confirm")}
      </Button>
    </form>
  );
}
