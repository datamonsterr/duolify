import { useState } from "react";
import { trackPrivacyControl } from "../../lib/analytics";
import { useUiPreferences } from "../design/localization/Preferences";
import { accountDelete, accountExport } from "../guided-chat/account.scripts";
import { trackGuidedChat } from "../guided-chat/analytics";
import { GuidedScriptChat } from "../guided-chat/GuidedScriptChat";
import { useOnline } from "../guided-chat/useOnline";
import type { Value } from "../guided-chat/types";
import { encryptAccountExport } from "./encrypted-export";
import { profileApi } from "./learner-context";
import { AuthError, credentialStore, session } from "./session";

const reauth = (cause: unknown) =>
  cause instanceof AuthError &&
  cause.status === 403 &&
  (cause.details as { code?: string } | undefined)?.code === "reauth_required";

/** Export and deletion as chats. Same endpoints, encryption and reauth rule as the old dialogs. */
export function GuidedAccountData({
  mode,
  onClose,
}: {
  mode: "export" | "delete";
  onClose: () => void;
}) {
  const { t } = useUiPreferences();
  const online = useOnline();
  const [needsReauth, setNeedsReauth] = useState(false);
  const [done, setDone] = useState(false);
  const script = mode === "export" ? accountExport : accountDelete;

  async function submit(answers: Record<string, Value>) {
    setNeedsReauth(false);
    try {
      if (mode === "export") {
        trackPrivacyControl("export_data");
        const data = await profileApi.exportData();
        const encrypted = await encryptAccountExport(
          data,
          String(answers.passphrase ?? ""),
        );
        const url = URL.createObjectURL(
          new Blob([encrypted], { type: "application/octet-stream" }),
        );
        const link = document.createElement("a");
        link.href = url;
        link.download = `duolify-data-${new Date().toISOString().slice(0, 10)}.json`;
        link.click();
        window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
        setDone(true);
        return;
      }
      trackPrivacyControl("delete_account");
      await profileApi.deleteAccount("DELETE");
      await credentialStore.write(null);
      try {
        localStorage.clear();
        sessionStorage.clear();
      } catch {
        /* Storage may be disabled. */
      }
      session.clear();
      window.location.assign("/sign-in");
    } catch (cause) {
      if (reauth(cause)) setNeedsReauth(true);
      throw cause;
    }
  }

  return (
    <div className="settings-scrim" onClick={onClose}>
      <section
        role="dialog"
        aria-modal="true"
        aria-label={t(mode === "export" ? "g.export.title" : "g.delete.title")}
        className="settings-dialog guided-account"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          if (e.key === "Escape") onClose();
        }}
      >
        <GuidedScriptChat
          script={script}
          online={online}
          onEvent={(event) => trackGuidedChat(script, "settings", event)}
          onSubmit={submit}
          onCancel={onClose}
          focusOnMount
        />
        {done && (
          <p role="status">
            {t("g.export.done")}{" "}
            <button type="button" className="gc-link" onClick={onClose}>
              {t("guided.cancel")}
            </button>
          </p>
        )}
        {needsReauth && (
          <p>
            <a href="/sign-in?returnTo=%2Fsettings">{t("g.account.reauth")}</a>
          </p>
        )}
      </section>
    </div>
  );
}
