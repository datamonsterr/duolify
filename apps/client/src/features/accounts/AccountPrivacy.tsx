import { useState } from "react";
import { trackPrivacyControl } from "../../lib/analytics";
import { Button } from "../design/Button";
import { Feedback } from "../design/chat/Feedback";
import { profileApi } from "./learner-context";
import { AuthError, credentialStore, session } from "./session";
import { encryptAccountExport } from "./encrypted-export";

export function AccountPrivacy() {
  const [exportOpen, setExportOpen] = useState(false);
  const [exportPassphrase, setExportPassphrase] = useState("");
  const [deletionOpen, setDeletionOpen] = useState(false);
  const [confirmation, setConfirmation] = useState("");
  const [privacyPending, setPrivacyPending] = useState(false);
  const [privacyError, setPrivacyError] = useState("");
  const [privacyNotice, setPrivacyNotice] = useState("");
  const [reauthRequired, setReauthRequired] = useState(false);

  function handlePrivacyFailure(cause: unknown, fallback: string) {
    if (
      cause instanceof AuthError &&
      cause.status === 403 &&
      (cause.details as { code?: string } | undefined)?.code ===
        "reauth_required"
    ) {
      setExportOpen(false);
      setDeletionOpen(false);
      setExportPassphrase("");
      setConfirmation("");
      setReauthRequired(true);
    }
    setPrivacyError(cause instanceof Error ? cause.message : fallback);
  }

  async function downloadExport() {
    if (privacyPending) return;
    setPrivacyPending(true);
    setPrivacyError("");
    try {
      trackPrivacyControl("export_data");
      const data = await profileApi.exportData();
      const encrypted = await encryptAccountExport(data, exportPassphrase);
      const url = URL.createObjectURL(
        new Blob([encrypted], { type: "application/octet-stream" }),
      );
      const link = document.createElement("a");
      link.href = url;
      link.download = `duolify-data-${new Date().toISOString().slice(0, 10)}.json`;
      link.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
      setExportOpen(false);
      setExportPassphrase("");
      setPrivacyNotice(
        "Encrypted export downloaded. Keep your passphrase safe; it cannot be recovered.",
      );
    } catch (cause) {
      handlePrivacyFailure(
        cause,
        "Could not prepare export. Connect and retry.",
      );
    } finally {
      setPrivacyPending(false);
    }
  }

  async function deleteAccount() {
    if (privacyPending || confirmation !== "DELETE") return;
    setPrivacyPending(true);
    setPrivacyError("");
    try {
      trackPrivacyControl("delete_account");
      await profileApi.deleteAccount(confirmation);
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
      handlePrivacyFailure(
        cause,
        "Could not request account removal. Connect and retry.",
      );
    } finally {
      setPrivacyPending(false);
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      setExportOpen(false);
      setDeletionOpen(false);
    }
  };

  return (
    <>
      {privacyError && <Feedback tone="danger" title={privacyError} />}
      {privacyNotice && <p role="status">{privacyNotice}</p>}
      {reauthRequired && (
        <p>
          Confirm your identity with a fresh sign-in, then return here to retry.{" "}
          <a href="/sign-in?returnTo=%2Fsettings">Verify your identity</a>
        </p>
      )}
      <section aria-label="Account data and privacy">
        <h2>Account</h2>
        <p>
          Download a protected copy of your account data or remove your account.
        </p>
        <div className="settings-actions">
          <Button
            type="button"
            onClick={() => {
              setPrivacyError("");
              setExportOpen(true);
            }}
          >
            Export data
          </Button>
          <Button
            type="button"
            onClick={() => {
              setPrivacyError("");
              setDeletionOpen(true);
            }}
          >
            Delete account
          </Button>
        </div>
      </section>

      {exportOpen && (
        <div className="settings-scrim" onClick={() => setExportOpen(false)}>
          <section
            role="dialog"
            aria-modal="true"
            aria-labelledby="export-title"
            className="settings-dialog"
            onClick={(e) => e.stopPropagation()}
            onKeyDown={handleKeyDown}
          >
            <h2 id="export-title">Prepare your export</h2>
            <p>
              Your export is encrypted before it is saved. Use a passphrase you
              can keep safe.
            </p>
            <label>
              Export passphrase
              <input
                aria-label="Export passphrase"
                type="password"
                autoComplete="new-password"
                value={exportPassphrase}
                onChange={(event) => setExportPassphrase(event.target.value)}
              />
            </label>
            <div className="settings-actions">
              <Button type="button" onClick={() => setExportOpen(false)}>
                Cancel
              </Button>
              <Button
                type="button"
                pending={privacyPending}
                onClick={downloadExport}
              >
                Download encrypted export
              </Button>
            </div>
          </section>
        </div>
      )}

      {deletionOpen && (
        <div
          className="settings-scrim"
          onClick={() => {
            setDeletionOpen(false);
            setConfirmation("");
          }}
        >
          <section
            role="dialog"
            aria-modal="true"
            aria-labelledby="deletion-title"
            className="settings-dialog"
            onClick={(e) => e.stopPropagation()}
            onKeyDown={handleKeyDown}
          >
            <h2 id="deletion-title">Delete your account?</h2>
            <p>
              Your personal data is removed within 30 days. Financial records
              are retained pseudonymously. This action cannot be undone.
            </p>
            <p>
              Courses you shared are removed, but learners who copied them keep
              their copies, shown as created by a deleted user.
            </p>
            <label>
              Type DELETE to confirm
              <input
                aria-label="Type DELETE to confirm"
                value={confirmation}
                onChange={(event) => setConfirmation(event.target.value)}
              />
            </label>
            <div className="settings-actions">
              <Button
                type="button"
                onClick={() => {
                  setDeletionOpen(false);
                  setConfirmation("");
                }}
              >
                Cancel
              </Button>
              <Button
                type="button"
                disabled={confirmation !== "DELETE"}
                pending={privacyPending}
                onClick={deleteAccount}
              >
                Delete permanently
              </Button>
            </div>
          </section>
        </div>
      )}
    </>
  );
}
