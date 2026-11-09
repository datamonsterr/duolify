import { Link } from "@tanstack/react-router";
import { useId } from "react";
import { useUiPreferences } from "./Preferences";
import { Button } from "../Button";
import "./preferences.css";
export function PreferenceControls({
  testLocales = false,
}: {
  testLocales?: boolean;
}) {
  const languageId = useId();
  const appearanceId = useId();
  const { preferences, update, reset, retry, saveError, t } =
    useUiPreferences();
  return (
    <section className="preference-controls" aria-label={t("settings.title")}>
      <div className="preference-select">
        <label htmlFor={languageId}>{t("settings.language")}</label>
        <select
          id={languageId}
          data-control="locale"
          value={preferences.locale}
          onChange={(event) =>
            update({ locale: event.target.value as typeof preferences.locale })
          }
        >
          <option value="en">English</option>
          <option value="ar">العربية</option>
          {testLocales && (
            <option value="en-XA">English · expanded copy</option>
          )}
        </select>
      </div>
      <div className="preference-select">
        <label htmlFor={appearanceId}>{t("settings.appearance")}</label>
        <select
          id={appearanceId}
          data-control="appearance"
          value={preferences.appearance}
          onChange={(event) =>
            update({
              appearance: event.target.value as typeof preferences.appearance,
            })
          }
        >
          {(["system", "light", "dark"] as const).map((mode) => (
            <option key={mode} value={mode}>
              {t(`settings.${mode}`)}
            </option>
          ))}
        </select>
      </div>
      <label className="preference-motion">
        <input
          data-control="motion"
          type="checkbox"
          checked={preferences.reduceMotion}
          onChange={(event) => update({ reduceMotion: event.target.checked })}
        />
        {t("settings.motion")}
      </label>
      <p>{t("settings.motion_hint")}</p>
      <Button data-control="reset" variant="quiet" onClick={reset}>
        {t("settings.reset")}
      </Button>
      <p>{t("settings.local")}</p>
      {saveError && (
        <div role="alert">
          <p>{t("settings.error")}</p>
          <Button variant="quiet" onClick={retry}>
            {t("settings.retry")}
          </Button>
        </div>
      )}
    </section>
  );
}
export function SettingsPage() {
  const { t } = useUiPreferences();
  return (
    <>
      <h1>{t("settings.title")}</h1>
      <PreferenceControls />
      <Link className="shell-link" to="/settings/sessions">
        {t("settings.sessions")}
      </Link>
    </>
  );
}
