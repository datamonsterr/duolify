import { useCallback, useMemo, useState } from "react";
import { useUiPreferences } from "../design/localization/Preferences";
import { GuidedScriptChat } from "./GuidedScriptChat";
import { courseLevel } from "./course.scripts";
import { trackGuidedChat } from "./analytics";
import { useOnline } from "./useOnline";
import type { Draft } from "./engine";
import type { Value } from "./types";

const levels = ["beginner", "intermediate", "advanced"];

/** Per-course level as a two-step chat: tap a level, confirm. Same endpoint as the old form. */
export function GuidedCourseLevel({
  courseId,
  courseTitle,
  levelOverride,
  globalLevel,
  onSaveLevel,
}: {
  courseId: string;
  courseTitle: string;
  levelOverride: string;
  globalLevel: string;
  onSaveLevel: (courseId: string, level: string) => Promise<unknown>;
}) {
  const { t } = useUiPreferences();
  const online = useOnline();
  const [failed, setFailed] = useState(false);
  const [saved, setSaved] = useState<string | null>(null);
  const start = useMemo<Draft | null>(() => {
    const current = levelOverride || globalLevel;
    return levels.includes(current)
      ? {
          script_version: courseLevel.version,
          step_id: "",
          answers: { level: { value: current } },
        }
      : null;
  }, [levelOverride, globalLevel]);
  const submit = useCallback(
    async (answers: Record<string, Value>) => {
      setFailed(false);
      try {
        await onSaveLevel(courseId, String(answers.level));
        setSaved(String(answers.level));
      } catch (error) {
        setFailed(true);
        throw error;
      }
    },
    [courseId, onSaveLevel],
  );
  return (
    <section className="guided-level" aria-label={courseTitle}>
      <h3>{courseTitle}</h3>
      <GuidedScriptChat
        script={courseLevel}
        initialDraft={start}
        online={online}
        onEvent={(event) => trackGuidedChat(courseLevel, "profile", event)}
        onSubmit={submit}
      />
      {saved && (
        <p role="status" className="field-hint">
          {t("g.level.done")}
        </p>
      )}
      {failed && (
        <p role="alert" className="field-error">
          {t("g.level.error")}
        </p>
      )}
    </section>
  );
}
