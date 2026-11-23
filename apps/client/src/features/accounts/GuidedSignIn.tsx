import { useRef, useState } from "react";
import { Button } from "../design/Button";
import { useUiPreferences } from "../design/localization/Preferences";
import { GuidedScriptChat } from "../guided-chat/GuidedScriptChat";
import { authSignin } from "../guided-chat/signin.script";
import { trackGuidedChat } from "../guided-chat/analytics";
import { useOnline } from "../guided-chat/useOnline";

/** Email code sign-in as a guided chat. The inputs are real labelled fields so autofill, OTP
 * suggestions and password managers keep working. Nothing here is drafted or persisted. */
export function GuidedSignIn({
  requestCode,
  verifyCode,
}: {
  /** Both return an error message to show inline, or null on success. */
  requestCode: (email: string, age: boolean) => Promise<string | null>;
  verifyCode: (
    email: string,
    code: string,
    age: boolean,
  ) => Promise<string | null>;
}) {
  const { t } = useUiPreferences();
  const online = useOnline();
  const email = useRef("");
  const [notice, setNotice] = useState("");
  return (
    <div className="guided-signin">
      <GuidedScriptChat
        script={authSignin}
        focusOnMount
        online={online}
        onEvent={(event) => trackGuidedChat(authSignin, "sign_in", event)}
        onSubmit={() => Promise.resolve()}
        onStepCommit={async (stepId, answers) => {
          if (stepId === "email") {
            email.current = String(answers.email ?? "").trim();
            return null;
          }
          if (stepId === "age") {
            const failure = await requestCode(email.current, true);
            if (!failure) setNotice(t("g.signin.sent"));
            return failure;
          }
          return verifyCode(email.current, String(answers.code ?? ""), true);
        }}
        stepActions={(step) =>
          step.id === "code" ? (
            <div className="gc-actions">
              <Button
                variant="quiet"
                disabled={!online}
                onClick={() => {
                  void requestCode(email.current, true).then((failure) =>
                    setNotice(failure ?? t("g.signin.sent")),
                  );
                }}
              >
                {t("g.signin.resend")}
              </Button>
            </div>
          ) : null
        }
      />
      {notice && (
        <p role="status" id="auth-notice">
          {notice}
        </p>
      )}
    </div>
  );
}
