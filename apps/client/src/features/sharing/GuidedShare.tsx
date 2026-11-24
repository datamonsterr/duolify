import { useMemo } from "react";
import { FlowChat } from "../guided-chat/FlowChat";
import type { Draft } from "../guided-chat/engine";
import { courseShare } from "../guided-chat/share.scripts";
import type { Value } from "../guided-chat/types";

/** Visibility as a two-step chat. Same publish and revoke calls as the old radio group. */
export function GuidedShare({
  shared,
  onChange,
}: {
  shared: boolean;
  /** Resolves true when the change went through; false keeps the answers for a retry. */
  onChange: (next: "private" | "link") => Promise<boolean>;
}) {
  const start = useMemo<Draft>(
    () => ({
      script_version: courseShare.version,
      step_id: "",
      answers: { visibility: { value: shared ? "link" : "private" } },
    }),
    [shared],
  );
  async function submit(answers: Record<string, Value>) {
    const next = answers.visibility === "link" ? "link" : "private";
    if (next === (shared ? "link" : "private")) return;
    if (!(await onChange(next))) throw new Error("share failed");
  }
  return (
    <div className="sharing-visibility">
      <FlowChat
        script={courseShare}
        entryPoint="share"
        initialDraft={start}
        onSubmit={submit}
        doneKey="g.share.done"
      />
    </div>
  );
}
