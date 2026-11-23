import { useCallback } from "react";
import { GuidedScriptChat } from "./GuidedScriptChat";
import { courseSteer } from "./course.scripts";
import { trackGuidedChat } from "./analytics";
import { useOnline } from "./useOnline";
import type { Value } from "./types";

/** Adds a course instruction as a chat: scope, text, confirm. Screening, the published-copy fork
 * warning and the proposal quote stay in the existing panel; this only collects the input. */
export function GuidedSteerAdd({
  onAdd,
}: {
  /** Resolves when the instruction was sent (or the fork warning took over); rejects to retry. */
  onAdd: (input: { text: string; scope: string }) => Promise<void>;
}) {
  const online = useOnline();
  const submit = useCallback(
    (answers: Record<string, Value>) =>
      onAdd({ text: String(answers.text ?? ""), scope: String(answers.scope) }),
    [onAdd],
  );
  return (
    <GuidedScriptChat
      script={courseSteer}
      online={online}
      onEvent={(event) => trackGuidedChat(courseSteer, "course_steer", event)}
      onSubmit={submit}
    />
  );
}
