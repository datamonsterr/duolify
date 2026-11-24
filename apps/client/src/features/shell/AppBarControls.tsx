import { useId } from "react";
import { Icon } from "../design/Icon";
import {
  useUiPreferences,
  type Preferences,
} from "../design/localization/Preferences";
import "./app-bar.css";

/** Language and theme, side by side at the end of the app bar. */
export function AppBarControls() {
  const id = useId();
  const { preferences, theme, update, t } = useUiPreferences();
  return (
    <div className="app-bar-controls">
      <select
        id={id}
        className="app-bar-language"
        data-control="locale"
        aria-label={t("settings.language")}
        value={preferences.locale}
        onChange={(event) =>
          update({ locale: event.target.value as Preferences["locale"] })
        }
      >
        <option value="en">{t("lang.en")}</option>
        <option value="ar">{t("lang.ar")}</option>
      </select>
      <button
        type="button"
        className="app-bar-theme"
        data-control="theme"
        aria-label={t("shell.dark_theme")}
        aria-pressed={theme === "dark"}
        title={t("shell.dark_theme")}
        onClick={() =>
          update({ appearance: theme === "dark" ? "light" : "dark" })
        }
      >
        <Icon name="theme" />
      </button>
    </div>
  );
}
