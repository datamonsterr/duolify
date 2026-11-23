import { useCallback, useMemo, useState } from "react";
import { useUiPreferences } from "../design/localization/Preferences";
import { GuidedScriptChat } from "./GuidedScriptChat";
import { courseBrief } from "./brief.script";
import { answersToBrief, briefToDraft } from "./briefMapping";
import { trackGuidedChat } from "./analytics";
import { useGuidedDraft } from "./useGuidedDraft";
import { useOnline } from "./useOnline";
import { emptyBrief } from "../courses/brief";
import type { Brief, BriefRevision, BriefService } from "../courses/brief";
import type { Value } from "./types";

/** The course brief as a guided chat. Confirming calls the existing endpoint; no preview or
 * Credits are touched here (the preview keeps its own quote and consent). */
export function GuidedBrief({
  learnerId,
  seed,
  service,
  onConfirmed,
}: {
  learnerId: string;
  seed: Brief;
  service: BriefService;
  onConfirmed: (revision: BriefRevision, brief: Brief) => void;
}) {
  const { t } = useUiPreferences();
  const online = useOnline();
  const { ready, initial, onDraft, clear } = useGuidedDraft(
    courseBrief.id,
    learnerId,
  );
  const [failed, setFailed] = useState(false);
  const start = useMemo(
    () =>
      initial && Object.keys(initial.answers).length
        ? initial
        : briefToDraft(seed),
    [initial, seed],
  );
  const submit = useCallback(
    async (answers: Record<string, Value>) => {
      setFailed(false);
      try {
        const brief = answersToBrief(answers, emptyBrief());
        const revision = await service.confirm(brief);
        await clear();
        onConfirmed(revision, brief);
      } catch (error) {
        setFailed(true);
        throw error;
      }
    },
    [service, clear, onConfirmed],
  );
  if (!ready) return <p role="status" aria-busy="true" />;
  return (
    <div className="guided-brief">
      <GuidedScriptChat
        script={courseBrief}
        initialDraft={start}
        online={online}
        onDraft={onDraft}
        onEvent={(event) =>
          trackGuidedChat(courseBrief, "course_create", event)
        }
        onSubmit={submit}
      />
      {failed && (
        <p role="alert" className="field-error">
          {t("g.brief.error")}
        </p>
      )}
    </div>
  );
}
