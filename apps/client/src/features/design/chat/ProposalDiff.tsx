import { useUiPreferences } from "../localization/Preferences";
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
  outlineControls = false,
  disableApply = false,
}: {
  proposal: OutlineProposal;
  onApply: () => void;
  onDiscard: () => void;
  pending?: boolean;
  outlineControls?: boolean;
  disableApply?: boolean;
}) {
  const { t } = useUiPreferences();
  return (
    <section className="outline-proposal" aria-label={t("proposal.title")}>
      <h3>{t("proposal.suggestion")}</h3>
      <ul>
        {proposal.changes.map((change) => (
          <li key={change.id}>
            <Badge role={roles[change.kind]}>
              {t(`proposal.${change.kind}`)}
            </Badge>
            <span dir="auto">
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
          disabled={disableApply}
          onClick={() => {
            trackGuidedUi({
              event: "ui_control_activated",
              surface: "outline",
              controlId: outlineControls ? "proposal_apply" : "apply",
            });
            onApply();
          }}
        >
          {t("proposal.apply")}
        </Button>
        <Button
          variant="quiet"
          disabled={pending}
          onClick={() => {
            trackGuidedUi({
              event: "ui_control_activated",
              surface: "outline",
              controlId: outlineControls ? "proposal_discard" : "discard",
            });
            onDiscard();
          }}
        >
          {t("proposal.discard")}
        </Button>
      </div>
    </section>
  );
}
