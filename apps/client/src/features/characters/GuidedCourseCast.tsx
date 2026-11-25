import { useCallback, useEffect, useMemo, useState } from "react";
import { Button } from "../design/Button";
import { useUiPreferences } from "../design/localization/Preferences";
import { trackGuidedChat } from "../guided-chat/analytics";
import {
  CharacterChatContext,
  type CastChoice,
  type CharacterChatApi,
} from "../guided-chat/characterChat";
import { characterCast } from "../guided-chat/character.scripts";
import { GuidedScriptChat } from "../guided-chat/GuidedScriptChat";
import type { Step, Value } from "../guided-chat/types";
import { useOnline } from "../guided-chat/useOnline";
import "../guided-chat/messages.character";
import { getDefaultCastFixtures } from "./fixtures";
import { characterLibrary, type CharacterRecord } from "./library";
import "./characters.css";

const BUILT_IN = ["default-guide", "default-supporter", "default-companion"];
const skipped = (v: Value | undefined) => !v || v === "none" || v === "default";

/**
 * Course Cast as a chat: guide, then up to two supporting Characters. Choices are the built-in
 * cast plus the Learner's own Characters; the existing PUT saves it and spends nothing.
 */
export function GuidedCourseCast({
  courseId,
  onClose,
}: {
  courseId: string;
  onClose: () => void;
}) {
  const { t } = useUiPreferences();
  const online = useOnline();
  const [records, setRecords] = useState<CharacterRecord[] | null>(null);
  const [failed, setFailed] = useState(false);
  const [taken, setTaken] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    let live = true;
    characterLibrary
      .list()
      .then((items) => live && setRecords(items))
      .catch(() => live && setFailed(true));
    return () => {
      live = false;
    };
  }, []);
  const choices = useMemo<CastChoice[]>(() => {
    const builtIns = getDefaultCastFixtures().map((spec, i) => ({
      id: BUILT_IN[i]!,
      name: spec.info.name,
      record: { spec },
    }));
    return [
      ...builtIns,
      ...(records ?? []).map((r) => ({
        id: r.id,
        name: r.document?.info.name ?? r.spec.info.name,
        record: r,
      })),
    ];
  }, [records]);
  const api = useMemo<CharacterChatApi>(
    () => ({ cast: { choices, taken } }),
    [choices, taken],
  );
  const nameOf = useCallback(
    (step: Step, value: Value) => {
      if (!step.character || !("pick" in step.character)) return null;
      if (value === "default") return t("g.cast.default");
      if (value === "none") return t("g.cast.none");
      return choices.find((c) => c.id === value)?.name ?? null;
    },
    [choices, t],
  );
  const commit = useCallback(
    async (_step: string, answers: Record<string, Value>) => {
      setTaken(
        Object.fromEntries(
          ["guide", "support1"]
            .map((id) => [id, answers[id]] as const)
            .filter(
              (e): e is readonly [string, string] =>
                typeof e[1] === "string" && !skipped(e[1]),
            ),
        ),
      );
      return null;
    },
    [],
  );
  const submit = useCallback(
    async (answers: Record<string, Value>) => {
      const guide = skipped(answers.guide) ? "" : String(answers.guide);
      const supporting = guide
        ? [answers.support1, answers.support2].filter(
            (v): v is string => typeof v === "string" && !skipped(v),
          )
        : [];
      await characterLibrary.select(courseId, { guide, supporting });
      setSaved(true);
    },
    [courseId],
  );
  return (
    <section className="gc-host" aria-label={t("g.cast.title")}>
      <div className="gc-host-bar">
        <h1>{t("g.cast.title")}</h1>
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
      {failed && <p role="alert">{t("g.cast.error")}</p>}
      {!records && !failed && <p role="status">{t("guided.loading")}</p>}
      {records && (
        <CharacterChatContext.Provider value={api}>
          <GuidedScriptChat
            script={characterCast}
            online={online}
            needsNetwork={false}
            onEvent={(e) => trackGuidedChat(characterCast, "course_cast", e)}
            onStepCommit={commit}
            onSubmit={submit}
            describe={nameOf}
            focusOnMount
          />
        </CharacterChatContext.Provider>
      )}
      {saved && (
        <div className="gc-host-done">
          <p role="status">{t("g.cast.saved")}</p>
          <Button onClick={onClose}>{t("g.char.done")}</Button>
        </div>
      )}
    </section>
  );
}
