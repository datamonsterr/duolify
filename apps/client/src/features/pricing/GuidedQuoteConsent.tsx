import { FlowChat } from "../guided-chat/FlowChat";
import { creditsConfirm } from "../guided-chat/share.scripts";

/**
 * Quote consent as a chat. The price lines above it are the panel's own, unchanged; only the
 * explicit confirm accepts, and accepting still reserves nothing.
 */
export function GuidedQuoteConsent({
  onAccept,
  onCancel,
}: {
  /** Resolves true when the terms were accepted. */
  onAccept: () => Promise<boolean>;
  onCancel: () => void;
}) {
  return (
    <FlowChat
      script={creditsConfirm}
      entryPoint="quote"
      onCancel={onCancel}
      onSubmit={async () => {
        if (!(await onAccept())) throw new Error("accept failed");
      }}
    />
  );
}
