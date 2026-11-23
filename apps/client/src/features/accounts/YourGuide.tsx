import { useState } from "react";
import {
  MASCOT_ACCESSORIES,
  MASCOT_SWATCHES,
  MASCOT_SWATCH_COLORS,
  MAX_ACCESSORIES,
  Mascot,
  getMascotPreference,
  setMascotPreference,
  useMascotPreference,
  type MascotAccessory,
  type MascotPreference,
} from "../../components/mascot";
import { trackMascot } from "../../lib/analytics";
import { useOwnCharacters } from "../characters/ownCharacters";
import { useUiPreferences } from "../design/localization/Preferences";
import type { MessageId } from "../design/localization/messages";
import { profileApi } from "./learner-context";
import { changedFields, toServerMascotPatch } from "./mascot-settings";

/** "Your guide": palette allowlist, up to two accessories, one of the Learner's own Characters. Free, no model. */
export function YourGuide() {
  const { t } = useUiPreferences();
  const pref = useMascotPreference();
  const [failed, setFailed] = useState(false);
  // Characters are offered only when the Learner has some (feature-detected; v2 documents slot in unchanged).
  const own = useOwnCharacters() ?? [];

  async function save(patch: Partial<MascotPreference>) {
    const before = getMascotPreference();
    setFailed(false);
    setMascotPreference({ ...before, ...patch });
    try {
      await profileApi.saveSettings({ mascot: toServerMascotPatch(patch) });
      const fields = changedFields(patch);
      if (fields.length)
        trackMascot({
          event: "mascot_customized",
          mode: patch.mode ?? before.mode,
          fields_changed: fields.join(","),
        });
      if (patch.companionVisible !== undefined)
        trackMascot({
          event: "mascot_companion_toggled",
          visible: patch.companionVisible,
        });
    } catch {
      setMascotPreference(before);
      setFailed(true);
    }
  }
  const full = pref.accessories.length >= MAX_ACCESSORIES;
  const swatches = [undefined, ...MASCOT_SWATCHES] as const;
  return (
    <section className="your-guide">
      <Mascot
        size={96}
        state="happy"
        appearance={{
          swatch: pref.swatch,
          accessories: pref.accessories,
          characterId: pref.mode === "character" ? pref.characterId : undefined,
        }}
      />
      <fieldset>
        <legend>{t("mascot.colour")}</legend>
        {swatches.map((id) => (
          <label key={id ?? "default"} className="your-guide-swatch">
            <input
              type="radio"
              name="guide-colour"
              checked={pref.swatch === id}
              onChange={() =>
                void save({
                  swatch: id,
                  mode: "default",
                  characterId: undefined,
                })
              }
            />
            <span
              aria-hidden="true"
              style={{
                background: id ? MASCOT_SWATCH_COLORS[id].body : "var(--lime)",
              }}
            />
            {t(`mascot.swatch.${id ?? "default"}` as MessageId)}
          </label>
        ))}
      </fieldset>
      <fieldset>
        <legend>
          {t("mascot.extras")} · {t("mascot.extras_max")}
        </legend>
        {MASCOT_ACCESSORIES.map((a: MascotAccessory) => {
          const on = pref.accessories.includes(a);
          return (
            <label key={a}>
              <input
                type="checkbox"
                checked={on}
                disabled={!on && full}
                onChange={() =>
                  void save({
                    accessories: on
                      ? pref.accessories.filter((x) => x !== a)
                      : [...pref.accessories, a],
                  })
                }
              />
              {t(`mascot.acc.${a}` as MessageId)}
            </label>
          );
        })}
      </fieldset>
      {own.length > 0 && (
        <label>
          {t("mascot.character")}
          <select
            value={pref.mode === "character" ? (pref.characterId ?? "") : ""}
            onChange={(e) =>
              void save(
                e.target.value
                  ? { mode: "character", characterId: e.target.value }
                  : { mode: "default", characterId: undefined },
              )
            }
          >
            <option value="">{t("mascot.character_default")}</option>
            {own.map((c) => (
              <option key={c.id} value={c.id}>
                {c.spec.info.name}
              </option>
            ))}
          </select>
        </label>
      )}
      <label>
        <input
          type="checkbox"
          checked={pref.companionVisible}
          onChange={(e) => void save({ companionVisible: e.target.checked })}
        />
        {t("mascot.companion")}
      </label>
      {failed && <p role="alert">{t("mascot.save_error")}</p>}
    </section>
  );
}
