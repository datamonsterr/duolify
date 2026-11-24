import { useCallback, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { Button } from "../design/Button";
import { useUiPreferences } from "../design/localization/Preferences";
import { trackGuidedChat } from "../guided-chat/analytics";
import type { Draft } from "../guided-chat/engine";
import { GuidedScriptChat } from "../guided-chat/GuidedScriptChat";
import { profileGoal } from "../guided-chat/profile.scripts";
import type { Answers, Value } from "../guided-chat/types";
import { useOnline } from "../guided-chat/useOnline";
import {
  profileApi,
  type LearnerSettings,
  type SettingsFields,
} from "./learner-context";

type GoalFields = Pick<
  SettingsFields,
  "daily_minutes" | "daily_xp" | "reminder_enabled" | "reminder_time"
>;

/** Settings row: current goal in words, plus the button that opens the chat dialog. Lives in
 * this lazy module so its copy loads with the catalogue. */
export function GuidedDailyGoalRow({
  settings,
  onSaved,
}: {
  settings: LearnerSettings;
  onSaved: (fields: GoalFields) => void;
}) {
  const { t } = useUiPreferences();
  const [open, setOpen] = useState(false);
  return (
    <div className="settings-daily-goal">
      <p>
        {t("g.goal.summary", {
          minutes: settings.daily_minutes ?? "–",
          xp: settings.daily_xp ?? "–",
        })}{" "}
        {settings.reminder_enabled
          ? t("g.goal.reminder_on", { time: settings.reminder_time })
          : t("g.goal.reminder_off")}
      </p>
      <Button type="button" variant="secondary" onClick={() => setOpen(true)}>
        {t("g.goal.title")}
      </Button>
      {open && (
        <GuidedDailyGoal
          settings={settings}
          onSaved={onSaved}
          onClose={() => setOpen(false)}
        />
      )}
    </div>
  );
}

/** Daily goal and reminder time as a chat dialog. Saves through the settings endpoint. */
export function GuidedDailyGoal({
  settings,
  onSaved,
  onClose,
}: {
  settings: LearnerSettings;
  onSaved: (fields: GoalFields) => void;
  onClose: () => void;
}) {
  const { t } = useUiPreferences();
  const online = useOnline();
  const [done, setDone] = useState(false);
  const [failed, setFailed] = useState(false);
  const start = useMemo<Draft>(() => {
    const answers: Answers = {};
    if (settings.daily_minutes)
      answers.daily_minutes = { value: settings.daily_minutes };
    if (settings.daily_xp) answers.daily_xp = { value: settings.daily_xp };
    if (settings.reminder_enabled !== null) {
      answers.reminder_on = { value: settings.reminder_enabled };
      if (settings.reminder_enabled && settings.reminder_time)
        answers.reminder_time = { value: settings.reminder_time };
    }
    return { script_version: profileGoal.version, step_id: "", answers };
  }, [settings]);
  const submit = useCallback(
    async (answers: Record<string, Value>) => {
      setFailed(false);
      const fields: Partial<GoalFields> = {};
      if (typeof answers.daily_minutes === "number")
        fields.daily_minutes = answers.daily_minutes;
      if (typeof answers.daily_xp === "number")
        fields.daily_xp = answers.daily_xp;
      if (typeof answers.reminder_on === "boolean") {
        fields.reminder_enabled = answers.reminder_on;
        if (answers.reminder_on && typeof answers.reminder_time === "string")
          fields.reminder_time = answers.reminder_time;
      }
      try {
        const saved = await profileApi.saveSettings(fields);
        onSaved({
          daily_minutes: saved.daily_minutes,
          daily_xp: saved.daily_xp,
          reminder_enabled: saved.reminder_enabled,
          reminder_time: saved.reminder_time,
        });
        setDone(true);
      } catch (error) {
        setFailed(true);
        throw error;
      }
    },
    [onSaved],
  );
  // Portal: the Settings row sits inside the settings form, and a chat must not nest in it.
  return createPortal(
    <div className="settings-scrim" onClick={onClose}>
      <section
        role="dialog"
        aria-modal="true"
        aria-label={t("g.goal.title")}
        className="settings-dialog guided-account"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          if (e.key === "Escape") onClose();
        }}
      >
        <GuidedScriptChat
          script={profileGoal}
          initialDraft={start}
          online={online}
          onEvent={(event) => trackGuidedChat(profileGoal, "settings", event)}
          onSubmit={submit}
          focusOnMount
        />
        {done && (
          <p role="status">
            {t("g.goal.done")}{" "}
            <button type="button" className="gc-link" onClick={onClose}>
              {t("guided.cancel")}
            </button>
          </p>
        )}
        {failed && (
          <p role="alert" className="field-error">
            {t("g.goal.error")}
          </p>
        )}
        {!done && (
          <button type="button" className="gc-link" onClick={onClose}>
            {t("guided.cancel")}
          </button>
        )}
      </section>
    </div>,
    document.body,
  );
}
