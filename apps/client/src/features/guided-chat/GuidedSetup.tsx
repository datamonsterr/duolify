import { useCallback, useMemo, useState } from "react";
import { useUiPreferences } from "../design/localization/Preferences";
import type { MessageId } from "../design/localization/messages";
import type {
  ContextFields,
  LearnerContext,
} from "../accounts/learner-context";
import { GuidedScriptChat } from "./GuidedScriptChat";
import { onboardingSetup } from "./onboarding.script";
import { answersToFields, profileToDraft } from "./setupMapping";
import { useGuidedDraft } from "./useGuidedDraft";
import { useOnline } from "./useOnline";
import { trackGuidedChat } from "./analytics";
import type { Value } from "./types";

const goalOptions = ["career", "school", "hobby", "travel"];

/** Onboarding and "update by chat" as a guided chat. Same endpoint as the old form. */
export function GuidedSetup({
  profile,
  onCommit,
  entryPoint = "onboarding",
}: {
  profile: LearnerContext;
  onCommit: (
    action: "save" | "skip",
    fields: Partial<ContextFields>,
  ) => Promise<LearnerContext>;
  entryPoint?: "onboarding" | "profile";
}) {
  const { t } = useUiPreferences();
  const { ready, initial, onDraft, clear } = useGuidedDraft(
    onboardingSetup.id,
    profile.learner_id,
  );
  const online = useOnline();
  const [done, setDone] = useState(false);
  const [failed, setFailed] = useState(false);
  const label = useCallback((key: string) => t(key as MessageId), [t]);
  // A saved draft wins; otherwise open on the Learner's current profile.
  const start = useMemo(
    () =>
      initial && Object.keys(initial.answers).length
        ? initial
        : profileToDraft(profile),
    [initial, profile],
  );
  const submit = useCallback(
    async (answers: Record<string, Value>) => {
      setFailed(false);
      try {
        if (Object.keys(answers).length === 0) await onCommit("skip", {});
        else
          await onCommit("save", answersToFields(answers, label, goalOptions));
        await clear();
      } catch (error) {
        setFailed(true);
        throw error;
      }
    },
    [onCommit, label, clear],
  );
  if (done)
    return (
      <div className="learner-context">
        <h1 tabIndex={-1}>{t("g.setup.done")}</h1>
        <p>{t("g.setup.done_note")}</p>
        <a className="shell-link" href="/profile">
          {t("g.setup.profile")}
        </a>
        <a className="shell-link" href="/">
          {t("g.setup.start")}
        </a>
      </div>
    );
  if (!ready) return <p role="status" aria-busy="true" />;
  return (
    <div className="guided-setup">
      <GuidedScriptChat
        script={onboardingSetup}
        initialDraft={start}
        online={online}
        onDraft={onDraft}
        onEvent={(event) => trackGuidedChat(onboardingSetup, entryPoint, event)}
        onSubmit={submit}
        onDone={() => setDone(true)}
      />
      {failed && (
        <p role="alert" className="field-error">
          {t("g.setup.error")}
        </p>
      )}
    </div>
  );
}
