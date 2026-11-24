import { FlowChat } from "../guided-chat/FlowChat";
import { reportContent } from "../guided-chat/share.scripts";
import type { Value } from "../guided-chat/types";

/** Reason and optional details as a chat. The API call, reasons and failures are unchanged. */
export function GuidedReport({
  onSend,
}: {
  /** Rejects on failure; the sheet shows the neutral reason. */
  onSend: (reason: string, details: string) => Promise<void>;
}) {
  return (
    <FlowChat
      script={reportContent}
      entryPoint="marketplace"
      focusOnMount
      onSubmit={(answers: Record<string, Value>) =>
        onSend(String(answers.reason ?? ""), String(answers.details ?? ""))
      }
    />
  );
}
