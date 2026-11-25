import { FlowChat } from "../guided-chat/FlowChat";
import { courseInvite } from "../guided-chat/share.scripts";
import type { Value } from "../guided-chat/types";
import { cleanName } from "./invites";

/** Invite by username as a chat. The friend list and sent list stay on the panel. */
export function GuidedInvite({
  onSend,
}: {
  /** Resolves true when the invite was sent. */
  onSend: (username: string) => Promise<boolean>;
}) {
  async function submit(answers: Record<string, Value>) {
    const username = cleanName(String(answers.username ?? ""));
    if (!username || !(await onSend(username)))
      throw new Error("invite failed");
  }
  return (
    <FlowChat
      script={courseInvite}
      entryPoint="share"
      onSubmit={submit}
      againKey="g.invite.again"
      doneKey="g.invite.done"
    />
  );
}
