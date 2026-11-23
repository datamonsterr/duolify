import { useState } from "react";
import { Button } from "../design/Button";
import { useUiPreferences } from "../design/localization/Preferences";
import { CharacterPreview } from "./CharacterPreview";
import { CharacterSelector, type Tab } from "./CharacterSelector";
import { applyDraftPatch, defaultDraft } from "./draft";
import { characterLibrary, type CharacterRecord } from "./library";
import type { CharactersV2Id } from "./messages";
import { POSES, type RigView } from "./rig-v2";
import { validateSpecV2 } from "./schema-v2";
import {
  EXPRESSIONS_V2,
  type CharacterSpecV2,
  type ExpressionV2,
  type Pose,
} from "./types-v2";

/** Selector-driven creator and editor. The preview, selector and draft contract are standalone. */
export function CharacterCreator({
  record,
  onSaved,
  onClose,
}: {
  record?: CharacterRecord;
  onSaved: (record: CharacterRecord) => void;
  onClose: () => void;
}) {
  const { t: raw } = useUiPreferences();
  const t = raw as (id: CharactersV2Id) => string;
  const [spec, setSpec] = useState<CharacterSpecV2>(
    () => record?.document ?? defaultDraft("human"),
  );
  const [tab, setTab] = useState<Tab>("kind");
  const [view, setView] = useState<RigView>("avatar");
  const [pose, setPose] = useState<Pose>("idle");
  const [expression, setExpression] = useState<ExpressionV2>("idle");
  const [rejected, setRejected] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const valid = validateSpecV2(spec).valid && spec.info.name.trim() !== "";
  const submit = async () => {
    setPending(true);
    setError("");
    try {
      const out = record
        ? await characterLibrary.editV2(record, spec)
        : await characterLibrary.createV2(spec);
      setSaved(true);
      onSaved(out);
    } catch (e) {
      setError(e instanceof Error ? e.message : "unavailable");
    } finally {
      setPending(false);
    }
  };
  return (
    <section className="cv2-creator" aria-label={t("characters.v2.title")}>
      <div className="cv2-stage">
        <CharacterPreview
          spec={spec}
          view={view}
          pose={pose}
          expression={expression}
        />
        <div className="cv2-controls">
          <div role="group" aria-label={t("characters.v2.preview")}>
            {(["avatar", "body"] as const).map((v) => (
              <button
                key={v}
                type="button"
                aria-pressed={view === v}
                onClick={() => setView(v)}
              >
                {t(`characters.v2.view.${v}` as CharactersV2Id)}
              </button>
            ))}
          </div>
          <label>
            <span>{t("characters.v2.pose")}</span>
            <select
              value={pose}
              onChange={(e) => setPose(e.target.value as Pose)}
            >
              {POSES.map((p) => (
                <option key={p}>{p}</option>
              ))}
            </select>
          </label>
          <label>
            <span>{t("characters.v2.expression")}</span>
            <select
              value={expression}
              onChange={(e) => setExpression(e.target.value as ExpressionV2)}
            >
              {EXPRESSIONS_V2.map((x) => (
                <option key={x}>{x}</option>
              ))}
            </select>
          </label>
        </div>
      </div>
      <div className="cv2-side">
        <CharacterSelector
          spec={spec}
          tab={tab}
          onTab={setTab}
          onReplace={(next) => {
            setSpec(next);
            setRejected(false);
          }}
          onPatch={(patch) => {
            const r = applyDraftPatch(spec, patch);
            setRejected(!r.ok);
            if (r.ok) setSpec(r.spec);
          }}
        />
        {rejected && <p role="alert">{t("characters.v2.invalid")}</p>}
        {error && <p role="alert">{error}</p>}
        {saved && <p role="status">{t("characters.v2.saved")}</p>}
        <div className="cv2-actions">
          <Button
            onClick={() => void submit()}
            pending={pending}
            disabled={!valid}
          >
            {t(record ? "characters.v2.save" : "characters.v2.use")}
          </Button>
          <Button variant="secondary" onClick={onClose}>
            {t("characters.v2.close")}
          </Button>
        </div>
      </div>
    </section>
  );
}
