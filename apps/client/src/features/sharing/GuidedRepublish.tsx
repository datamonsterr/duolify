import { FlowChat } from "../guided-chat/FlowChat";
import { courseRepublish } from "../guided-chat/share.scripts";
import type { Value } from "../guided-chat/types";

/** Changelog as a chat. Screening, limits and the result banners stay on the panel. */
export function GuidedRepublish({
  onPublish,
}: {
  /** Resolves true when the request went through (any screening verdict). */
  onPublish: (changelog: string) => Promise<boolean>;
}) {
  async function submit(answers: Record<string, Value>) {
    if (!(await onPublish(String(answers.changelog ?? ""))))
      throw new Error("republish failed");
  }
  return (
    <FlowChat
      script={courseRepublish}
      entryPoint="share"
      onSubmit={submit}
      againKey="g.republish.again"
      doneKey="g.republish.done"
    />
  );
}
