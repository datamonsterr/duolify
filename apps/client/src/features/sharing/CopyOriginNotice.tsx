import { originMessage, type CopyOrigin } from "./retention";
import "./sharing.css";

/** Creator line on a copy: the name while the account exists, "deleted user" afterwards. */
export function CopyOriginNotice({ origin }: { origin?: CopyOrigin | null }) {
  const message = originMessage(origin);
  if (!message) return null;
  return (
    <aside className="copy-origin" role="note">
      <strong>{message.title}</strong>
      {message.detail && <span>{message.detail}</span>}
    </aside>
  );
}
