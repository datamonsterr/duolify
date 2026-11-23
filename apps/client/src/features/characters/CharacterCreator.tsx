import { useState } from "react";
import { Button } from "../design/Button";
import { useCharacterT } from "./useCharacterT";
import { CharacterPreview } from "./CharacterPreview";
import {
  CharacterSelector,
  CharacterTabs,
  type Tab,
} from "./CharacterSelector";
import { applyDraftPatch, defaultDraft } from "./draft";
import { characterLibrary, type CharacterRecord } from "./library";
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
  const t = useCharacterT();
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
  const nextPose = () => {
    const i = (POSES.indexOf(pose) + 1) % POSES.length;
    setPose(POSES[i]!);
    setExpression(EXPRESSIONS_V2[i % EXPRESSIONS_V2.length]!);
  };
  return (
    <section className="cv2-creator" aria-label={t("characters.v2.title")}>
      <header className="cv2-head">
        <div className="cv2-bar">
          <button
            type="button"
            className="cv2-icon"
            aria-label={t("characters.v2.close")}
            onClick={onClose}
          >
            <Icon d="M15 5l-7 7 7 7" />
          </button>
          <h1>{t("characters.v2.short")}</h1>
          <Button
            className="cv2-save"
            onClick={() => void submit()}
            pending={pending}
            disabled={!valid}
          >
            <Icon d="M5 12l5 5 9-10" />
            {t(record ? "characters.v2.save" : "characters.v2.use")}
          </Button>
        </div>
        <div className="cv2-stage">
          <button
            type="button"
            className="cv2-avatar"
            aria-pressed={view === "body"}
            aria-label={t(
              view === "avatar"
                ? "characters.v2.view.body"
                : "characters.v2.view.avatar",
            )}
            onClick={() => setView(view === "avatar" ? "body" : "avatar")}
          >
            <CharacterPreview
              spec={spec}
              view={view}
              pose={pose}
              expression={expression}
            />
          </button>
          <button
            type="button"
            className="cv2-icon"
            aria-label={t("characters.v2.pose")}
            onClick={nextPose}
          >
            <Icon d="M4 12a8 8 0 0 1 14-5l2-2v6h-6l2.5-2.5A5 5 0 1 0 17 14" />
          </button>
        </div>
        <CharacterTabs tab={tab} onTab={setTab} />
      </header>
      <div className="cv2-side">
        <CharacterSelector
          spec={spec}
          tab={tab}
          onTab={setTab}
          hideTabs
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
      </div>
    </section>
  );
}

function Icon({ d }: { d: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      width="22"
      height="22"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={d} />
    </svg>
  );
}
