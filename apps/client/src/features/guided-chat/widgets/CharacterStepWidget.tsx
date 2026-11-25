import { Button } from "../../design/Button";
import { useUiPreferences } from "../../design/localization/Preferences";
import type { MessageId } from "../../design/localization/messages";
import { CastAvatar } from "../../characters/CastAvatar";
import { CharacterSelector } from "../../characters/CharacterSelector";
import { CharacterStage } from "../../characters/CharacterStage";
import { useCharacterT } from "../../characters/useCharacterT";
import "../../characters/characters.css";
import { useCharacterChat } from "../characterChat";
import type { Step } from "../types";

/** Selector tab or Cast pick, rendered as one chat widget. Lazy: only scripts that ask load it. */
export default function CharacterStepWidget({
  step,
  promptId,
  disabled,
  value,
  onChange,
}: {
  step: Step;
  promptId: string;
  disabled?: boolean;
  value: string;
  onChange: (value: string) => void;
}) {
  const { t } = useUiPreferences();
  const ct = useCharacterT();
  const { draft, cast } = useCharacterChat();
  const label = t((step.actionKey ?? "guided.confirm") as MessageId);
  const submit = (
    <div className="gc-character-actions">
      <Button type="submit" disabled={disabled || (!draft && !value)}>
        {label}
      </Button>
    </div>
  );
  const spec = step.character;
  if (spec && "tab" in spec && draft)
    return (
      <div role="group" aria-labelledby={promptId} className="gc-character">
        <CharacterStage spec={draft.spec} tall />
        <CharacterSelector
          spec={draft.spec}
          tab={spec.tab}
          hideTabs
          onPatch={draft.patch}
          onReplace={draft.replace}
        />
        {draft.rejected && <p role="alert">{ct("characters.v2.invalid")}</p>}
        {submit}
      </div>
    );
  if (spec && "pick" in spec && cast) {
    const none = step.id === "guide" ? "default" : "none";
    const rows = [
      {
        id: none,
        name: t(
          (step.id === "guide" ? "g.cast.default" : "g.cast.none") as MessageId,
        ),
        record: null,
      },
      ...cast.choices
        .filter(
          (c) =>
            !Object.entries(cast.taken).some(
              ([stepId, id]) => stepId !== step.id && id === c.id,
            ),
        )
        .map((c) => ({ id: c.id, name: c.name, record: c.record })),
    ];
    return (
      <div className="gc-character">
        <div className="cv2-tiles" role="radiogroup" aria-labelledby={promptId}>
          {rows.map((r) => (
            <label key={r.id} className="cv2-tile">
              <input
                type="radio"
                name={`${promptId}-pick`}
                value={r.id}
                checked={value === r.id}
                aria-label={r.name}
                onChange={() => onChange(r.id)}
              />
              {r.record ? (
                <CastAvatar
                  spec={r.record.spec}
                  document={r.record.document}
                  size={56}
                />
              ) : (
                <svg
                  className="cv2-none"
                  viewBox="0 0 24 24"
                  width="56"
                  height="56"
                  aria-hidden="true"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.5"
                  strokeLinecap="round"
                >
                  <circle cx="12" cy="12" r="8" />
                  <path d="M6.5 17.5l11-11" />
                </svg>
              )}
              <span>{r.name}</span>
            </label>
          ))}
        </div>
        {submit}
      </div>
    );
  }
  return null;
}
