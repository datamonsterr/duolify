import { setAutoSend } from "../voice/autosend";
import { requestReplay } from "../onboarding/handoff";
import { navigateTo } from "../../lib/navigation";
import { session } from "./session";
import {
  useEffect,
  useState,
  useSyncExternalStore,
  type ChangeEvent,
} from "react";
import {
  setAnalyticsConsent,
  trackAppearanceChanged,
  trackPreference,
} from "../../lib/analytics";
import { Button } from "../design/Button";
import { Icon, type IconName } from "../design/Icon";
import { useMobileLayout } from "../shell/AdaptiveLayouts";
import { Feedback } from "../design/chat/Feedback";
import { YourGuide } from "./YourGuide";
import { PreferenceControls } from "../design/localization/PreferenceControls";
import { resolveLocale } from "../design/localization/messages";
import { useUiPreferences } from "../design/localization/Preferences";
import { PrivacySettings } from "../social/PrivacySettings";
import { SocialIdentity } from "../social/SocialIdentity";
import { AccountPrivacy } from "./AccountPrivacy";
import {
  emptySettings,
  profileApi,
  type LearnerSettings,
  type SettingsFields,
} from "./learner-context";
import { setFeedbackSettings } from "../design/feedback/feedbackBridge";
import { analyticsOptOut, setAnalyticsOptOut } from "./session";
import { applyPlaybackSettings } from "../lesson-player/immersive/settings";
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
  "fullscreen_on_rotate",
  "autoplay",
  "auto_send_after_dictation",
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
  const { t, preferences, update: updateUi, reset } = useUiPreferences();
  // Saving the account's language and appearance applies them to the app, not only this form.
  const applyToApp = (settings: LearnerSettings) => {
    const next = {
      locale: resolveLocale(settings.locale),
      appearance: settings.appearance,
      reduceMotion: settings.motion === "reduce",
    };
    if (
      next.locale !== preferences.locale ||
      next.appearance !== preferences.appearance ||
      next.reduceMotion !== preferences.reduceMotion
    )
      updateUi(next);
  };
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
          setFeedbackSettings({
            ui_sounds: value.ui_sounds,
            haptics: value.haptics,
          });
          applyVisualPreferences(value);
          applyPlaybackSettings(value);
        }
      })
      .catch(() => {
        if (active) setError(t("settings.read_error"));
      });
    return () => {
      active = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload only on retry, not on language change
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
      setFeedbackSettings({
        ui_sounds: result.ui_sounds,
        haptics: result.haptics,
      });
      if (!optedOut && saved.appearance !== result.appearance)
        trackAppearanceChanged(result.appearance);
      if (!optedOut && saved.ui_sounds !== result.ui_sounds)
        trackPreference("ui_sounds");
      if (!optedOut && saved.haptics !== result.haptics)
        trackPreference("haptics");
      applyVisualPreferences(result);
      applyPlaybackSettings(result);
      applyToApp(result);
      setNotice(t("settings.saved"));
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : t("settings.save_error"),
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

  const mobile = useMobileLayout();
  const section = useSettingsSection();
  // Phones open one section from the list; larger screens always show one, "look" first.
  const current = mobile ? section : (section ?? "look");
  const shown = (id: SectionId) => current === id;
  const inForm =
    current === "look" ||
    current === "audio" ||
    current === "goals" ||
    current === "privacy";
  const sectionTitle = (id: SectionId) => t(sectionLabels[id]);
  const bool =
    (key: keyof SettingsFields) => (event: ChangeEvent<HTMLInputElement>) =>
      update(key, event.target.checked);

  return (
    <section className="settings-page" aria-busy={pending}>
      <header className="settings-head">
        {mobile && section && (
          <button
            type="button"
            className="settings-back"
            aria-label={t("settings.back")}
            onClick={() => history.back()}
          >
            <Icon name="back" />
          </button>
        )}
        <h1 className={mobile ? undefined : "sr-only"}>
          {mobile && section ? sectionTitle(section) : t("settings.title")}
        </h1>
      </header>
      {!mobile && (
        <nav className="settings-tabs" aria-label={t("settings.title")}>
          {sections.map(({ id, icon }) => (
            <a
              key={id}
              href={`#${id}`}
              aria-current={current === id ? "page" : undefined}
            >
              <Icon name={icon} />
              <span>{sectionTitle(id)}</span>
            </a>
          ))}
        </nav>
      )}
      {!saved && !error && <p role="status">{t("settings.loading")}</p>}
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
              {t("settings.retry_load")}
            </Button>
          )}
        </>
      )}
      {mobile && !section && (
        <nav className="settings-hub" aria-label={t("settings.title")}>
          <ul>
            {sections.map(({ id, icon }) => (
              <li key={id}>
                <a href={`#${id}`} className="settings-row">
                  <Icon name={icon} accent="secondary" />
                  <span>{sectionTitle(id)}</span>
                  <Icon name="chevron" className="settings-chevron" />
                </a>
              </li>
            ))}
          </ul>
          <Button
            variant="quiet"
            onClick={() => {
              requestReplay(session.current()?.id ?? "");
              navigateTo("/");
            }}
          >
            {t("onboarding.replay")}
          </Button>
        </nav>
      )}
      {saved && (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void commit();
          }}
        >
          <fieldset hidden={!shown("look")} data-section="look">
            <legend>{sectionTitle("look")}</legend>
            <PreferenceControls
              onReset={() => {
                reset();
                update("locale", "en");
                update("appearance", "system");
                update("motion", "system");
              }}
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
            <label>
              {t("settings.text_size")}
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
            <details>
              <summary>{t("mascot.guide")}</summary>
              <YourGuide />
            </details>
          </fieldset>
          <fieldset hidden={!shown("audio")} data-section="audio">
            <legend>{sectionTitle("audio")}</legend>
            <label>
              <input
                type="checkbox"
                checked={draft.narration}
                onChange={bool("narration")}
              />
              {t("settings.narration")}
            </label>
            <label>
              {t("settings.speed")}
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
                onChange={bool("captions")}
              />
              {t("settings.captions")}
            </label>
            <label>
              <input
                type="checkbox"
                checked={draft.autoplay}
                onChange={bool("autoplay")}
              />
              {t("settings.autoplay")}
            </label>
            <label>
              <input
                type="checkbox"
                data-testid="setting-auto-send"
                checked={draft.auto_send_after_dictation}
                onChange={(event) => {
                  update("auto_send_after_dictation", event.target.checked);
                  setAutoSend(event.target.checked);
                }}
              />
              {t("voice.auto_send")}
            </label>
            <label>
              <input
                type="checkbox"
                checked={draft.fullscreen_on_rotate}
                onChange={bool("fullscreen_on_rotate")}
              />
              {t("settings.rotate_full")}
            </label>
            <label htmlFor="settings-ui-sounds">
              <input
                id="settings-ui-sounds"
                aria-label={t("settings.ui_sounds")}
                type="checkbox"
                checked={draft.ui_sounds}
                onChange={bool("ui_sounds")}
              />
              {t("settings.ui_sounds")}
            </label>
            <label htmlFor="settings-haptics">
              {t("settings.haptics")}
              <select
                id="settings-haptics"
                aria-label={t("settings.haptics")}
                value={draft.haptics}
                onChange={(event) =>
                  update(
                    "haptics",
                    event.target.value as LearnerSettings["haptics"],
                  )
                }
              >
                <option value="system">{t("settings.system")}</option>
                <option value="on">{t("settings.on")}</option>
                <option value="off">{t("settings.off")}</option>
              </select>
            </label>
          </fieldset>
          <fieldset hidden={!shown("goals")} data-section="goals">
            <legend>{sectionTitle("goals")}</legend>
            <label>
              {t("settings.locale")}
              <input
                aria-label={t("settings.locale")}
                value={draft.locale}
                onChange={(event) => update("locale", event.target.value)}
              />
            </label>
            <label>
              {t("settings.time_zone")}
              <input
                aria-label={t("settings.time_zone")}
                value={draft.time_zone}
                onChange={(event) => update("time_zone", event.target.value)}
              />
            </label>
            <label>
              {t("settings.daily_xp")}
              <input
                aria-label={t("settings.daily_xp")}
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
              {t("settings.daily_minutes")}
              <input
                aria-label={t("settings.daily_minutes")}
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
                onChange={bool("reminder_enabled")}
              />
              {t("settings.reminders")}
            </label>
            <label>
              {t("settings.reminder_time")}
              <input
                aria-label={t("settings.reminder_time")}
                type="time"
                value={draft.reminder_time}
                onChange={(event) =>
                  update("reminder_time", event.target.value)
                }
              />
            </label>
          </fieldset>
          <fieldset hidden={!shown("privacy")} data-section="privacy">
            <legend>{t("settings.sec_privacy")}</legend>
            <label>
              <input
                type="checkbox"
                checked={!draft.analytics_opt_out}
                onChange={(event) => changeAnalytics(event.target.checked)}
              />
              {t("settings.analytics")}
            </label>
          </fieldset>
          <div
            className="settings-actions action-dock"
            hidden={mobile && !inForm}
          >
            <Button type="submit" data-action-level="primary" pending={pending}>
              {t("settings.save")}
            </Button>
            <span role="status">{notice}</span>
          </div>
        </form>
      )}
      <div hidden={!shown("privacy")} data-section="privacy">
        <PrivacySettings />
        <AccountPrivacy />
      </div>
      <div hidden={!shown("account")} data-section="account">
        <SocialIdentity compact />
        <a className="shell-link" href="/settings/sessions">
          {t("settings.sessions")}
        </a>
        <a className="shell-link" href="/settings/characters">
          {t("settings.characters")}
        </a>
        {!mobile && (
          <Button
            variant="quiet"
            onClick={() => {
              requestReplay(session.current()?.id ?? "");
              navigateTo("/");
            }}
          >
            {t("onboarding.replay")}
          </Button>
        )}
      </div>
    </section>
  );
}

type SectionId = "look" | "audio" | "goals" | "privacy" | "account";
const sectionLabels = {
  look: "settings.sec_look",
  audio: "settings.sec_audio",
  goals: "settings.sec_goals",
  privacy: "settings.sec_privacy",
  account: "settings.sec_account",
} as const;
const sections: { id: SectionId; icon: IconName }[] = [
  { id: "look", icon: "look" },
  { id: "audio", icon: "audio" },
  { id: "goals", icon: "goal" },
  { id: "privacy", icon: "lock" },
  { id: "account", icon: "profile" },
];
const ids = sections.map((s) => s.id) as string[];
const readSection = () => {
  const id = window.location.hash.slice(1);
  return ids.includes(id) ? (id as SectionId) : null;
};
function subscribeHash(callback: () => void) {
  window.addEventListener("hashchange", callback);
  return () => window.removeEventListener("hashchange", callback);
}
// The open section lives in the URL hash so the system back gesture closes it.
function useSettingsSection() {
  return useSyncExternalStore(subscribeHash, readSection, () => null);
}
