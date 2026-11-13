import { useEffect, useState } from "react";
import {
  setAnalyticsConsent,
  trackAppearanceChanged,
} from "../../lib/analytics";
import { Button } from "../design/Button";
import { Feedback } from "../design/chat/Feedback";
import { PreferenceControls } from "../design/localization/PreferenceControls";
import { resolveLocale } from "../design/localization/messages";
import { useUiPreferences } from "../design/localization/Preferences";
import { SocialIdentity } from "../social/SocialIdentity";
import { AccountPrivacy } from "./AccountPrivacy";
import {
  emptySettings,
  profileApi,
  type LearnerSettings,
  type SettingsFields,
} from "./learner-context";
import { analyticsOptOut, setAnalyticsOptOut } from "./session";
import "./settings.css";

const settingNames: (keyof SettingsFields)[] = [
  "analytics_opt_out",
  "appearance",
  "motion",
  "narration",
  "narration_speed",
  "captions",
  "text_size",
  "ui_sounds",
  "haptics",
  "locale",
  "time_zone",
  "daily_minutes",
  "daily_xp",
  "reminder_enabled",
  "reminder_time",
];

function applyVisualPreferences(settings: LearnerSettings) {
  const root = document.documentElement;
  if (settings.appearance === "system") root.removeAttribute("data-theme");
  else root.dataset.theme = settings.appearance;
  if (settings.motion === "system") root.removeAttribute("data-motion");
  else root.dataset.motion = settings.motion;
  root.style.fontSize = `${settings.text_size * 100}%`;
}

export function Settings() {
  const { t } = useUiPreferences();
  const [saved, setSaved] = useState<LearnerSettings | null>(null);
  const [draft, setDraft] = useState<LearnerSettings>(emptySettings);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let active = true;
    void profileApi
      .readSettings()
      .then((value) => {
        if (active) {
          const optedOut = value.analytics_opt_out || analyticsOptOut();
          const current = { ...value, analytics_opt_out: optedOut };
          setSaved(value);
          setDraft(current);
          setAnalyticsOptOut(optedOut);
          setAnalyticsConsent(!optedOut);
          applyVisualPreferences(value);
        }
      })
      .catch(() => {
        if (active) setError("Could not read settings. Connect and retry.");
      });
    return () => {
      active = false;
    };
  }, [attempt]);

  const update = <K extends keyof LearnerSettings>(
    key: K,
    value: LearnerSettings[K],
  ) => {
    setDraft((current) => ({ ...current, [key]: value }));
    setNotice("");
  };

  async function commit() {
    if (pending || !saved) return;
    const optedOut = draft.analytics_opt_out;
    if (optedOut) {
      setAnalyticsOptOut(true);
      setAnalyticsConsent(false);
    }
    setPending(true);
    setError("");
    try {
      const fields = Object.fromEntries(
        settingNames
          .filter((key) => saved[key] !== draft[key])
          .map((key) => [key, draft[key]]),
      ) as Partial<SettingsFields>;
      const result = await profileApi.saveSettings(fields);
      setSaved(result);
      setDraft(result);
      setAnalyticsOptOut(optedOut);
      if (!optedOut) setAnalyticsConsent(true);
      if (!optedOut && saved.appearance !== result.appearance)
        trackAppearanceChanged(result.appearance);
      applyVisualPreferences(result);
      setNotice("Settings saved");
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Could not save settings. Retry.",
      );
      if (!optedOut && saved.analytics_opt_out) setAnalyticsConsent(false);
    } finally {
      setPending(false);
    }
  }

  function changeAnalytics(enabled: boolean) {
    const optedOut = !enabled;
    update("analytics_opt_out", optedOut);
    if (optedOut) {
      setAnalyticsOptOut(true);
      setAnalyticsConsent(false);
    }
  }

  return (
    <section className="settings-page" aria-busy={pending}>
      <h1>{t("settings.title")}</h1>
      <p>Appearance, lessons, accessibility and privacy.</p>
      {!saved && !error && <p role="status">Loading settings…</p>}
      {error && (
        <>
          <Feedback tone="danger" title={error} blocking />
          {!saved && (
            <Button
              onClick={() => {
                setError("");
                setAttempt((value) => value + 1);
              }}
            >
              Retry settings
            </Button>
          )}
        </>
      )}
      {saved && (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void commit();
          }}
        >
          <PreferenceControls
            value={{
              locale: resolveLocale(draft.locale),
              appearance: draft.appearance,
              reduceMotion: draft.motion === "reduce",
            }}
            onChange={(patch) => {
              if (patch.locale) update("locale", patch.locale);
              if (patch.appearance) update("appearance", patch.appearance);
              if (patch.reduceMotion !== undefined)
                update("motion", patch.reduceMotion ? "reduce" : "system");
            }}
          />
          <fieldset>
            <legend>Lessons &amp; audio</legend>
            <label>
              <input
                type="checkbox"
                checked={draft.narration}
                onChange={(event) => update("narration", event.target.checked)}
              />
              Narration
            </label>
            <label>
              Narration speed
              <select
                value={draft.narration_speed}
                onChange={(event) =>
                  update("narration_speed", Number(event.target.value))
                }
              >
                {[0.75, 1, 1.25, 1.5, 1.75].map((speed) => (
                  <option key={speed} value={speed}>
                    {speed}×
                  </option>
                ))}
              </select>
            </label>
            <label>
              <input
                type="checkbox"
                checked={draft.captions}
                onChange={(event) => update("captions", event.target.checked)}
              />
              Captions
            </label>
            <label>
              Text size
              <select
                value={draft.text_size}
                onChange={(event) =>
                  update("text_size", Number(event.target.value))
                }
              >
                {[0.8, 0.9, 1, 1.1, 1.2, 1.3, 1.4, 1.5].map((size) => (
                  <option key={size} value={size}>
                    {Math.round(size * 100)}%
                  </option>
                ))}
              </select>
            </label>
            <label>
              <input
                type="checkbox"
                checked={draft.ui_sounds}
                onChange={(event) => update("ui_sounds", event.target.checked)}
              />
              UI sounds
            </label>
            <label>
              Haptics
              <select
                value={draft.haptics}
                onChange={(event) =>
                  update(
                    "haptics",
                    event.target.value as LearnerSettings["haptics"],
                  )
                }
              >
                <option value="system">System default</option>
                <option value="on">On</option>
                <option value="off">Off</option>
              </select>
            </label>
          </fieldset>
          <fieldset>
            <legend>Privacy</legend>
            <label>
              <input
                type="checkbox"
                checked={!draft.analytics_opt_out}
                onChange={(event) => changeAnalytics(event.target.checked)}
              />
              Analytics
            </label>
            <p>Turning analytics off stops future usage events.</p>
          </fieldset>
          <fieldset>
            <legend>Learning defaults</legend>
            <label>
              Locale
              <input
                aria-label="Locale"
                value={draft.locale}
                onChange={(event) => update("locale", event.target.value)}
              />
            </label>
            <label>
              Time zone
              <input
                aria-label="Time zone"
                value={draft.time_zone}
                onChange={(event) => update("time_zone", event.target.value)}
              />
            </label>
            <label>
              Daily XP goal
              <input
                aria-label="Daily XP goal"
                type="number"
                min="1"
                max="10000"
                value={draft.daily_xp ?? ""}
                onChange={(event) =>
                  update(
                    "daily_xp",
                    event.target.value === ""
                      ? null
                      : Number(event.target.value),
                  )
                }
              />
            </label>
            <label>
              Daily minutes
              <input
                aria-label="Daily minutes"
                type="number"
                min="1"
                max="1440"
                value={draft.daily_minutes ?? ""}
                onChange={(event) =>
                  update(
                    "daily_minutes",
                    event.target.value === ""
                      ? null
                      : Number(event.target.value),
                  )
                }
              />
            </label>
            <label>
              <input
                type="checkbox"
                checked={draft.reminder_enabled ?? false}
                onChange={(event) =>
                  update("reminder_enabled", event.target.checked)
                }
              />
              Reminders
            </label>
            <label>
              Reminder time
              <input
                aria-label="Reminder time"
                type="time"
                value={draft.reminder_time}
                onChange={(event) =>
                  update("reminder_time", event.target.value)
                }
              />
            </label>
          </fieldset>
          <div className="settings-actions">
            <Button type="submit" pending={pending}>
              Save settings
            </Button>
            <span role="status">{notice}</span>
          </div>
        </form>
      )}
      <SocialIdentity compact />
      <AccountPrivacy />
      <a className="shell-link" href="/profile">
        Profile
      </a>
      <a className="shell-link" href="/settings/sessions">
        {t("settings.sessions")}
      </a>
      <a className="shell-link" href="/settings/characters">
        Private characters
      </a>
    </section>
  );
}
