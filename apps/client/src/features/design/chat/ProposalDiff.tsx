import { trackGuidedUi } from "../../../lib/analytics";
import { Badge } from "../Badge";
import { Button } from "../Button";
import "./guided-chat.css";

export type OutlineChange = {
  id: string;
  kind: "added" | "removed" | "moved" | "retitled";
  description: string;
};
export type OutlineProposal = { id: string; changes: OutlineChange[] };
const labels = {
  added: "Added",
  removed: "Removed",
  moved: "Moved",
  retitled: "Retitled",
} as const;
const roles = {
  added: "success",
  removed: "danger",
  moved: "info",
  retitled: "warning",
} as const;
export function ProposalDiff({
  proposal,
  onApply,
  onDiscard,
  pending = false,
}: {
  proposal: OutlineProposal;
  onApply: () => void;
  onDiscard: () => void;
  pending?: boolean;
}) {
  return (
    <section className="outline-proposal" aria-label="Proposed outline changes">
      <h3>Here's my suggestion</h3>
      <ul>
        {proposal.changes.map((change) => (
          <li key={change.id}>
            <Badge role={roles[change.kind]}>{labels[change.kind]}</Badge>
            <span>
              {change.kind === "removed" ? (
                <s>{change.description}</s>
              ) : (
                change.description
              )}
            </span>
          </li>
        ))}
      </ul>
      <div className="guided-actions">
        <Button
          pending={pending}
          onClick={() => {
            trackGuidedUi({
              event: "ui_control_activated",
              surface: "outline",
              controlId: "apply",
            });
            onApply();
          }}
        >
          Apply
        </Button>
        <Button
          variant="quiet"
          disabled={pending}
          onClick={() => {
            trackGuidedUi({
              event: "ui_control_activated",
              surface: "outline",
              controlId: "discard",
            });
            onDiscard();
          }}
        >
          Discard
        </Button>
      </div>
    </section>
  );
}
