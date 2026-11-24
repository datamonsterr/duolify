import { useRef, useState } from "react";
import { Button } from "../design/Button";
import { useUiPreferences } from "../design/localization/Preferences";
import { GuidedScriptChat } from "../guided-chat/GuidedScriptChat";
import { authSignin } from "../guided-chat/signin.script";
import { trackGuidedChat } from "../guided-chat/analytics";
import { useOnline } from "../guided-chat/useOnline";
import { AuthError } from "./session";
import { isTurnstileFailure } from "./turnstile-config";
import { useTurnstileCheck } from "./turnstile-check";

/** Email code sign-in as a guided chat. The inputs are real labelled fields so autofill, OTP
 * suggestions and password managers keep working. Nothing here is drafted or persisted. */
export function GuidedSignIn({
  siteKey,
  reload,
  requestCode,
  verifyCode,
}: {
  siteKey: string | null;
  reload: () => void;
  requestCode: (email: string, age: boolean, proof?: string) => Promise<void>;
  verifyCode: (
    email: string,
    code: string,
    age: boolean,
    proof?: string,
  ) => Promise<void>;
}) {
  const { t } = useUiPreferences();
  const online = useOnline();
  const email = useRef("");
  const [notice, setNotice] = useState("");
  const [resending, setResending] = useState(false);
  const emailCheck = useTurnstileCheck(
    siteKey,
    "email_code",
    true,
    false,
    reload,
  );
  const signupCheck = useTurnstileCheck(
    siteKey,
    "sign_up",
    true,
    false,
    reload,
  );
  const resendCheck = useTurnstileCheck(
    siteKey,
    "email_code",
    true,
    true,
    reload,
  );

  async function submit(
    check: ReturnType<typeof useTurnstileCheck>,
    action: (proof?: string) => Promise<void>,
  ) {
    const proof = check.consume();
    if (proof === null) return false as const;
    try {
      await action(proof);
      check.reset();
      return null;
    } catch (cause) {
      if (cause instanceof AuthError && isTurnstileFailure(cause.details)) {
        check.fail();
        return false as const;
      }
      check.reset();
      return cause instanceof Error ? cause.message : t("guided.submit_failed");
    }
  }

  async function resend() {
    setNotice("");
    setResending(true);
    const failure = await submit(resendCheck, (proof) =>
      requestCode(email.current, true, proof),
    );
    if (failure !== false) setNotice(failure ?? t("g.signin.sent"));
    setResending(false);
  }
  return (
    <div className="guided-signin">
      <GuidedScriptChat
        script={authSignin}
        focusOnMount
        online={online}
        onEvent={(event) => trackGuidedChat(authSignin, "sign_in", event)}
        onSubmit={() => Promise.resolve()}
        stepDisabled={(step) =>
          (step.id === "age" && !emailCheck.ready) ||
          (step.id === "code" && !signupCheck.ready)
        }
        onStepCommit={async (stepId, answers) => {
          if (stepId === "email") {
            email.current = String(answers.email ?? "").trim();
            return null;
          }
          if (stepId === "age") {
            const failure = await submit(emailCheck, (proof) =>
              requestCode(email.current, true, proof),
            );
            if (failure === null) setNotice(t("g.signin.sent"));
            return failure;
          }
          return submit(signupCheck, (proof) =>
            verifyCode(email.current, String(answers.code ?? ""), true, proof),
          );
        }}
        stepActions={(step) =>
          step.id === "age" ? (
            emailCheck.widget
          ) : step.id === "code" ? (
            <>
              {signupCheck.widget}
              <div className="gc-actions">
                <Button
                  variant="quiet"
                  pending={resending}
                  disabled={!online || !resendCheck.ready}
                  onClick={() => void resend()}
                >
                  {t("g.signin.resend")}
                </Button>
              </div>
              {resendCheck.widget}
            </>
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
