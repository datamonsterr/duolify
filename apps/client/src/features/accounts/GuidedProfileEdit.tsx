import { useCallback, useMemo, useState } from "react";
import { useUiPreferences } from "../design/localization/Preferences";
import type { MessageId } from "../design/localization/messages";
import { trackGuidedChat } from "../guided-chat/analytics";
import type { Draft } from "../guided-chat/engine";
import { GuidedScriptChat } from "../guided-chat/GuidedScriptChat";
import { profileEdit } from "../guided-chat/profile.scripts";
import type { Answers, Value } from "../guided-chat/types";
import { useOnline } from "../guided-chat/useOnline";
import type { ContextFields, LearnerContext } from "./learner-context";

const goalIds = ["career", "school", "hobby", "travel"];

/** Profile edit as a chat. Opens on the saved values; same endpoint as the old form. */
export function GuidedProfileEdit({
  profile,
  onCommit,
}: {
  profile: LearnerContext;
  onCommit: (
    action: "save" | "skip",
    fields: Partial<ContextFields>,
  ) => Promise<LearnerContext>;
}) {
  const { t } = useUiPreferences();
  const online = useOnline();
  const [done, setDone] = useState(false);
  const [failed, setFailed] = useState(false);
  const start = useMemo<Draft>(() => {
    const answers: Answers = {};
    for (const id of ["goal", "topic", "background", "level"] as const)
      if (profile[id]) answers[id] = { value: profile[id] };
    return { script_version: profileEdit.version, step_id: "", answers };
  }, [profile]);
  const submit = useCallback(
    async (answers: Record<string, Value>) => {
      setFailed(false);
      setDone(false);
      const fields: Partial<ContextFields> = {};
      for (const id of ["goal", "topic", "background", "level"] as const) {
        const value = answers[id];
        if (typeof value !== "string") continue;
        fields[id] =
          id === "goal" && goalIds.includes(value)
            ? t(`g.setup.goal.${value}` as MessageId)
            : value;
      }
      try {
        await onCommit("save", fields);
        setDone(true);
      } catch (error) {
        setFailed(true);
        throw error;
      }
    },
    [onCommit, t],
  );
  return (
    <section className="guided-profile" aria-label={t("g.profile.title")}>
      <GuidedScriptChat
        script={profileEdit}
        initialDraft={start}
        online={online}
        onEvent={(event) => trackGuidedChat(profileEdit, "profile", event)}
        onSubmit={submit}
      />
      {done && <p role="status">{t("g.profile.done")}</p>}
      {failed && (
        <p role="alert" className="field-error">
          {t("g.profile.error")}
        </p>
      )}
    </section>
  );
}
