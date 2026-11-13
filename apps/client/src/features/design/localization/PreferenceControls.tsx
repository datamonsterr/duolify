import { useId } from "react";
import { useUiPreferences, type Preferences } from "./Preferences";
import { Button } from "../Button";
import "./preferences.css";

export function PreferenceControls({
  testLocales = false,
  value,
  onChange,
}: {
  testLocales?: boolean;
  value?: Preferences;
  onChange?: (patch: Partial<Preferences>) => void;
}) {
  const languageId = useId();
  const appearanceId = useId();
  const { preferences, update, reset, retry, saveError, t } =
    useUiPreferences();
  const selected = value ?? preferences;
  const change = onChange ?? update;
  const persisted = value !== undefined && onChange !== undefined;
  return (
    <section className="preference-controls" aria-label={t("settings.title")}>
      <div className="preference-select">
        <label htmlFor={languageId}>{t("settings.language")}</label>
        <select
          id={languageId}
          data-control="locale"
          value={selected.locale}
          onChange={(event) =>
            change({ locale: event.target.value as Preferences["locale"] })
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
          value={selected.appearance}
          onChange={(event) =>
            change({
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
          checked={selected.reduceMotion}
          onChange={(event) => change({ reduceMotion: event.target.checked })}
        />
        {t("settings.motion")}
      </label>
      <p>{t("settings.motion_hint")}</p>
      {!persisted && (
        <>
          <Button data-control="reset" variant="quiet" onClick={reset}>
            {t("settings.reset")}
          </Button>
          <p>{t("settings.local")}</p>
        </>
      )}
      {!persisted && saveError && (
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
