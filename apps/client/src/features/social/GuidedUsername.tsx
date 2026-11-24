import { useCallback, useState } from "react";
import { session } from "../accounts/session";
import { useUiPreferences } from "../design/localization/Preferences";
import { trackGuidedChat } from "../guided-chat/analytics";
import { GuidedScriptChat } from "../guided-chat/GuidedScriptChat";
import { socialUsername } from "../guided-chat/social.scripts";
import type { Value } from "../guided-chat/types";
import { useOnline } from "../guided-chat/useOnline";
import { normalizeUsername, validateUsername } from "./username";

/** Claim (PUT) or change (PATCH) the username as a chat. Same endpoints and rules as the form. */
export interface Identity {
  username: {
    username: string;
    display_name: string;
    avatar: { initial: string };
  } | null;
  rules: { min: number; max: number };
}

export function GuidedUsername({
  method,
  onSaved,
  onCancel,
}: {
  method: "PUT" | "PATCH";
  onSaved: (next: Identity) => void;
  onCancel?: () => void;
}) {
  const { t } = useUiPreferences();
  const online = useOnline();
  const [problem, setProblem] = useState("");
  const check = useCallback(
    async (_step: string, answers: Record<string, Value>) =>
      validateUsername(String(answers.username ?? "")),
    [],
  );
  const submit = useCallback(
    async (answers: Record<string, Value>) => {
      setProblem("");
      try {
        const next = await session.request<Identity>(
          "/v1/social/username",
          method,
          {
            username: normalizeUsername(String(answers.username ?? "")),
          },
        );
        onSaved(next);
      } catch (error) {
        setProblem(
          error instanceof Error && error.message
            ? error.message
            : t("g.username.error"),
        );
        throw error;
      }
    },
    [method, onSaved, t],
  );
  return (
    <section className="guided-username" aria-label={t("g.username.title")}>
      <GuidedScriptChat
        script={socialUsername}
        online={online}
        needsNetwork={false}
        onEvent={(event) => trackGuidedChat(socialUsername, "settings", event)}
        onStepCommit={check}
        onSubmit={submit}
        onCancel={onCancel}
      />
      {problem && (
        <p role="alert" className="social-error">
          {problem}
        </p>
      )}
      {onCancel && (
        <button type="button" className="gc-link" onClick={onCancel}>
          {t("guided.cancel")}
        </button>
      )}
    </section>
  );
}
