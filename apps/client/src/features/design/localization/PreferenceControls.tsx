import { useId } from "react";
import { useUiPreferences, type Preferences } from "./Preferences";
import { Button } from "../Button";
import { Icon } from "../Icon";
import "./preferences.css";

const modes = ["system", "light", "dark"] as const;
const modeIcons = { system: "theme", light: "sun", dark: "moon" } as const;

export function PreferenceControls({
  testLocales = false,
  value,
  onChange,
  onReset,
  hideReset = false,
  segmentedAppearance = false,
}: {
  testLocales?: boolean;
  value?: Preferences;
  onChange?: (patch: Partial<Preferences>) => void;
  /** Settings page: reset also clears the unsaved form. */
  onReset?: () => void;
  /** The caller renders its own reset button at the end of its section. */
  hideReset?: boolean;
  /** Settings: System / Light / Dark as one segmented control, placed first. */
  segmentedAppearance?: boolean;
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
      {segmentedAppearance && (
        <div
          className="preference-segmented"
          role="radiogroup"
          aria-label={t("settings.appearance")}
          data-control="appearance"
          onKeyDown={(event) => {
            const keys = ["ArrowRight", "ArrowLeft", "ArrowDown", "ArrowUp"];
            if (!keys.includes(event.key)) return;
            event.preventDefault();
            const forward =
              event.key === "ArrowDown" ||
              event.key ===
                (document.dir === "rtl" ? "ArrowLeft" : "ArrowRight");
            const at = modes.indexOf(selected.appearance);
            const next =
              modes[(at + (forward ? 1 : -1) + modes.length) % modes.length];
            change({ appearance: next });
            requestAnimationFrame(() =>
              event.currentTarget
                .querySelector<HTMLElement>('[aria-checked="true"]')
                ?.focus(),
            );
          }}
        >
          {modes.map((mode) => (
            <button
              key={mode}
              type="button"
              role="radio"
              aria-checked={selected.appearance === mode}
              tabIndex={selected.appearance === mode ? 0 : -1}
              onClick={() => change({ appearance: mode })}
            >
              <Icon name={modeIcons[mode]} />
              {t(`settings.${mode}`)}
            </button>
          ))}
        </div>
      )}
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
      {!segmentedAppearance && (
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
      )}
      <label className="preference-motion">
        <input
          data-control="motion"
          type="checkbox"
          checked={selected.reduceMotion}
          onChange={(event) => change({ reduceMotion: event.target.checked })}
        />
        {t("settings.motion")}
      </label>
      {!hideReset && (!persisted || onReset) && (
        <Button data-control="reset" variant="quiet" onClick={onReset ?? reset}>
          {t("settings.reset")}
        </Button>
      )}
      {!persisted && <p>{t("settings.local")}</p>}
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
