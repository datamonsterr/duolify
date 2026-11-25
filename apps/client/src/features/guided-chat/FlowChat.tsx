import { useState } from "react";
import { useUiPreferences } from "../design/localization/Preferences";
import { trackGuidedChat } from "./analytics";
import type { Draft } from "./engine";
import { GuidedScriptChat } from "./GuidedScriptChat";
import type { GuidedScript, Value } from "./types";
import { useOnline } from "./useOnline";

/** One guided script wired to analytics and the online flag. `again` offers a fresh run after done. */
export function FlowChat({
  script,
  entryPoint,
  onSubmit,
  onCancel,
  initialDraft,
  doneKey,
  againKey,
  focusOnMount,
}: {
  script: GuidedScript;
  entryPoint: string;
  onSubmit: (answers: Record<string, Value>) => Promise<void>;
  onCancel?: () => void;
  initialDraft?: Draft | null;
  /** Message id for the line shown after a successful submit. */
  doneKey?: string;
  /** Message id for the button that starts a fresh chat after done. */
  againKey?: string;
  focusOnMount?: boolean;
}) {
  const { t } = useUiPreferences();
  const online = useOnline();
  const [round, setRound] = useState(0);
  const [done, setDone] = useState(false);
  return (
    <>
      <GuidedScriptChat
        key={round}
        script={script}
        initialDraft={initialDraft}
        online={online}
        onEvent={(event) => trackGuidedChat(script, entryPoint, event)}
        onSubmit={onSubmit}
        onCancel={onCancel}
        onDone={() => setDone(true)}
        focusOnMount={focusOnMount}
      />
      {done && doneKey && (
        <p role="status" className="field-hint">
          {t(doneKey as never)}
          {againKey && (
            <>
              {" "}
              <button
                type="button"
                className="gc-link"
                onClick={() => {
                  setDone(false);
                  setRound((r) => r + 1);
                }}
              >
                {t(againKey as never)}
              </button>
            </>
          )}
        </p>
      )}
    </>
  );
}
