import { trackGuidedUi } from "../../../lib/analytics";
import { Button } from "../Button";
import { Field, Feedback } from "./Feedback";

export type SummaryLine = {
  id: string;
  label: string;
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
      <p>Updates as you chat. You can edit every line.</p>
      {lines.map((line) => (
        <Field
          key={line.id}
          label={line.label}
          value={line.value}
          required={line.required}
          error={line.error}
          hint={
            !line.value
              ? "Waiting for your answer. You can leave this blank."
              : undefined
          }
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
            ? "Confirming brief…"
            : confirmed
              ? "Brief confirmed"
              : unsaved
                ? "Unsaved changes"
                : "Review before confirming"
        }
        tone={confirmed ? "success" : "info"}
      />
      {error && <Feedback tone="danger" title={error} blocking />}
      <p>
        Nothing is saved or charged until you explicitly confirm in the course
        flow.
      </p>
      <Button type="submit" pending={pending}>
        Confirm brief
      </Button>
    </form>
  );
}
