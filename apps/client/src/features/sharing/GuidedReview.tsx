import { useMemo } from "react";
import { FlowChat } from "../guided-chat/FlowChat";
import type { Draft } from "../guided-chat/engine";
import { courseReview } from "../guided-chat/share.scripts";
import type { Value } from "../guided-chat/types";

/** Stars and an optional note as a chat. Opens on the learner's own review when there is one. */
export function GuidedReview({
  mine,
  maxChars,
  onSave,
}: {
  mine: { stars: number; text: string } | null;
  maxChars: number;
  /** Resolves true when the review was saved; false keeps the answers for a retry. */
  onSave: (stars: number, text: string) => Promise<boolean>;
}) {
  const start = useMemo<Draft | null>(
    () =>
      mine
        ? {
            script_version: courseReview.version,
            step_id: "",
            answers: {
              stars: { value: mine.stars },
              ...(mine.text ? { text: { value: mine.text } } : {}),
            },
          }
        : null,
    [mine],
  );
  async function submit(answers: Record<string, Value>) {
    const text = String(answers.text ?? "");
    if (Array.from(text).length > maxChars) throw new Error("too long");
    if (!(await onSave(Number(answers.stars), text)))
      throw new Error("review failed");
  }
  return (
    <FlowChat
      script={courseReview}
      entryPoint="marketplace"
      initialDraft={start}
      onSubmit={submit}
    />
  );
}
