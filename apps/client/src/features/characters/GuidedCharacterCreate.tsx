import { useCallback, useMemo, useRef, useState } from "react";
import { Button } from "../design/Button";
import { useUiPreferences } from "../design/localization/Preferences";
import { trackGuidedChat } from "../guided-chat/analytics";
import {
  CharacterChatContext,
  type CharacterChatApi,
} from "../guided-chat/characterChat";
import { characterCreate } from "../guided-chat/character.scripts";
import { GuidedScriptChat } from "../guided-chat/GuidedScriptChat";
import type { Step, Value } from "../guided-chat/types";
import { useOnline } from "../guided-chat/useOnline";
import "../guided-chat/messages.character";
import { CharacterStage } from "./CharacterStage";
import { applyDraftPatch, defaultDraft } from "./draft";
import { characterLibrary, type CharacterRecord } from "./library";
import type { CharacterSpecV2 } from "./types-v2";
import "./characters.css";

const PRONOUNS: Record<string, string> = {
  she: "she/her",
  he: "he/him",
  they: "they/them",
  none: "",
};

/**
 * Character creation as a chat. The draft document lives here; the selector widgets patch it
 * through the shared draft-patch contract and every patch is validated by the shared schema,
 * so a turn can never leave the draft invalid. Saving is the editor's free `createV2` call.
 */
export function GuidedCharacterCreate({
  onClose,
  onFullEditor,
  onSaved,
}: {
  onClose: () => void;
  onFullEditor?: () => void;
  onSaved?: (record: CharacterRecord) => void;
}) {
  const { t } = useUiPreferences();
  const online = useOnline();
  const [spec, setSpec] = useState<CharacterSpecV2>(() =>
    defaultDraft("human"),
  );
  const specRef = useRef(spec);
  const [rejected, setRejected] = useState(false);
  const [saved, setSaved] = useState<CharacterRecord | null>(null);
  const apply = useCallback((next: CharacterSpecV2) => {
    specRef.current = next;
    setSpec(next);
  }, []);
  const patch = useCallback(
    (p: Record<string, unknown>) => {
      const r = applyDraftPatch(specRef.current, p);
      setRejected(!r.ok);
      if (r.ok) apply(r.spec);
      return r.ok;
    },
    [apply],
  );
  const api = useMemo<CharacterChatApi>(
    () => ({
      draft: {
        spec,
        rejected,
        patch: (p) => void patch(p),
        replace: (next) => {
          setRejected(false);
          apply(next);
        },
      },
    }),
    [spec, rejected, patch, apply],
  );
  const commit = useCallback(
    async (stepId: string, answers: Record<string, Value>) => {
      if (stepId === "name") {
        const ok = patch({ info: { name: String(answers.name ?? "").trim() } });
        return ok ? null : t("g.char.name.error");
      }
      if (stepId === "pronouns") {
        patch({
          identity: { pronouns: PRONOUNS[String(answers.pronouns)] ?? "" },
        });
      }
      return null;
    },
    [patch, t],
  );
  const submit = useCallback(async () => {
    const record = await characterLibrary.createV2(specRef.current);
    setSaved(record);
    onSaved?.(record);
  }, [onSaved]);
  return (
    <section className="gc-host" aria-label={t("g.char.title")}>
      <div className="gc-host-bar">
        <h1>{t("g.char.title")}</h1>
        <button
          type="button"
          className="cv2-icon"
          aria-label={t("g.char.close")}
          onClick={onClose}
        >
          <svg
            viewBox="0 0 24 24"
            width="22"
            height="22"
            aria-hidden="true"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.5"
            strokeLinecap="round"
          >
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </button>
      </div>
      <CharacterChatContext.Provider value={api}>
        <GuidedScriptChat
          script={characterCreate}
          online={online}
          needsNetwork={false}
          onEvent={(e) => trackGuidedChat(characterCreate, "characters", e)}
          onStepCommit={commit}
          onSubmit={submit}
          stepActions={(step: Step) =>
            step.widget === "confirm" ? <CharacterStage spec={spec} /> : null
          }
          focusOnMount
        />
      </CharacterChatContext.Provider>
      {saved && (
        <div className="gc-host-done">
          <p role="status">{t("g.char.saved")}</p>
          <CharacterStage spec={saved.document ?? spec} />
          <Button onClick={onClose}>{t("g.char.done")}</Button>
        </div>
      )}
      {!saved && onFullEditor && (
        <Button variant="quiet" onClick={onFullEditor}>
          {t("g.char.full")}
        </Button>
      )}
    </section>
  );
}
