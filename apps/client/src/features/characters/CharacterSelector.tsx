import { useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { useCharacterT } from "./useCharacterT";
import { useUiPreferences } from "../design/localization/Preferences";
import {
  COLOR_PRESETS,
  SKIN_TONES,
  fieldOptions,
  KINDS,
  speciesOf,
} from "./catalog";
import { CharacterPreview } from "./CharacterPreview";
import { changeKind, changeSpecies } from "./draft";
import type { CharactersV2Id } from "./messages";
import { optionLabel } from "./messages";
import { validateSpecV2 } from "./schema-v2";
import type { BodyKind, CharacterSpecV2 } from "./types-v2";

export const TABS = ["kind", "look", "outfit", "about"] as const;
export type Tab = (typeof TABS)[number];

type Field =
  | { id: string; path: string[]; kind: "tiles" }
  | { id: string; path: string[]; kind: "color" }
  | { id: string; path: string[]; kind: "skin" }
  | { id: string; path: string[]; kind: "text"; multiline?: boolean }
  | { id: string; path: string[]; kind: "voice" }
  | { id: string; path: string[]; kind: "pronouns" }
  | { id: string; path: string[]; kind: "accessories" };

const f = (path: string[], kind: Field["kind"] = "tiles"): Field =>
  ({ id: path.join("_"), path, kind }) as Field;

/** Fields per tab and body kind. Option lists come from the schema and species table. */
export function fieldsFor(spec: CharacterSpecV2, tab: Tab): Field[] {
  const b = spec.body;
  if (tab === "kind") {
    return "species" in b ? [f(["body", "species"])] : [];
  }
  if (tab === "look") {
    const common = [
      f(["body", "age_band"]),
      f(["body", "build"]),
      f(["body", "height"]),
    ];
    const face = [
      f(["body", "eyes", "shape"]),
      f(["body", "eyes", "color"], "color"),
      f(["body", "mouth"]),
      f(["body", "glasses", "style"]),
      f(["body", "glasses", "color"], "color"),
    ];
    if (b.kind === "human")
      return [
        f(["body", "skin_tone"], "skin"),
        f(["body", "face_shape"]),
        f(["body", "hair", "length"]),
        f(["body", "hair", "style"]),
        f(["body", "hair", "texture"]),
        f(["body", "hair", "color"], "color"),
        f(["body", "head_covering", "style"]),
        f(["body", "head_covering", "color"], "color"),
        f(["body", "eyebrows", "style"]),
        f(["body", "nose"]),
        f(["body", "facial_hair", "style"]),
        f(["body", "facial_hair", "color"], "color"),
        ...face,
        ...common,
      ];
    const colors = [
      f(["body", "primary_color"], "color"),
      f(["body", "secondary_color"], "color"),
    ];
    if (b.kind === "animal")
      return [
        ...colors,
        f(["body", "markings"]),
        f(["body", "ears"]),
        f(["body", "tail"]),
        ...face,
        ...common,
      ];
    if (b.kind === "robot")
      return [
        f(["body", "base"]),
        ...colors,
        f(["body", "markings"]),
        f(["body", "antenna"]),
        f(["body", "locomotion"]),
        ...face,
        ...common,
      ];
    return [
      ...colors,
      f(["body", "wings"]),
      f(["body", "horns"]),
      f(["body", "tail"]),
      ...face,
      ...common,
    ];
  }
  if (tab === "outfit")
    return [
      f(["clothes", "top", "style"]),
      f(["clothes", "top", "color"], "color"),
      f(["clothes", "bottom", "style"]),
      f(["clothes", "bottom", "color"], "color"),
      f(["clothes", "shoes", "style"]),
      f(["clothes", "shoes", "color"], "color"),
      f(["clothes", "outerwear", "style"]),
      f(["clothes", "outerwear", "color"], "color"),
      f(["clothes", "pattern"]),
      f(["clothes", "accessories"], "accessories"),
    ];
  return [
    f(["info", "name"], "text"),
    f(["identity", "pronouns"], "pronouns"),
    f(["identity", "label"], "text"),
    f(["personality", "traits"], "text"),
    f(["personality", "speaking_style"], "text"),
    {
      id: "personality_persona_prompt",
      path: ["personality", "persona_prompt"],
      kind: "text",
      multiline: true,
    },
    f(["voice_key"], "voice"),
  ];
}

const getAt = (obj: unknown, path: string[]): unknown =>
  path.reduce<unknown>(
    (o, k) => (o as Record<string, unknown> | undefined)?.[k],
    obj,
  );
const toPatch = (path: string[], value: unknown): Record<string, unknown> =>
  path.reduceRight<unknown>((v, k) => ({ [k]: v }), value) as Record<
    string,
    unknown
  >;
const label = (t: (id: string) => string, id: string) =>
  t(`characters.v2.f.${id}`);

export interface SelectorProps {
  spec: CharacterSpecV2;
  /** RFC 7386 merge patch, the same contract the guided chat uses. */
  onPatch: (patch: Record<string, unknown>) => void;
  /** Whole-document replacement (kind and species switches reset to defaults). */
  onReplace: (next: CharacterSpecV2) => void;
  tab?: Tab;
  onTab?: (tab: Tab) => void;
  /** The host renders <CharacterTabs/> itself (for example in a sticky header). */
  hideTabs?: boolean;
}

export function CharacterSelector({
  spec,
  onPatch,
  onReplace,
  tab: controlled,
  onTab,
  hideTabs,
}: SelectorProps) {
  const t = useCharacterT();
  const {
    preferences: { locale },
  } = useUiPreferences();
  const [inner, setInner] = useState<Tab>("kind");
  const tab = controlled ?? inner;
  const setTab = (next: Tab) => (onTab ? onTab(next) : setInner(next));
  const uid = useId();
  return (
    <div className="cv2-selector">
      {!hideTabs && <CharacterTabs tab={tab} onTab={setTab} />}
      <div
        role="tabpanel"
        aria-labelledby={`cv2-tab-${tab}`}
        className="cv2-panel"
      >
        {tab === "kind" && (
          <Tiles
            name={`${uid}-kind`}
            legend={t("characters.v2.f.body_kind")}
            value={spec.body.kind}
            options={KINDS.map((k) => ({
              value: k,
              spec: changeKind(spec, k as BodyKind),
              ok: true,
            }))}
            locale={locale}
            onPick={(v) => onReplace(changeKind(spec, v as BodyKind))}
          />
        )}
        {tab === "kind" && "species" in spec.body && (
          <Tiles
            name={`${uid}-species`}
            legend={t("characters.v2.f.body_species")}
            value={spec.body.species}
            options={speciesOf(spec.body.kind).map((s) => ({
              value: s,
              spec: changeSpecies(spec, s),
              ok: true,
            }))}
            locale={locale}
            onPick={(v) => onReplace(changeSpecies(spec, v))}
          />
        )}
        {tab !== "kind" &&
          fieldsFor(spec, tab).map((field) => (
            <FieldView
              key={field.id + spec.body.kind}
              field={field}
              spec={spec}
              uid={uid}
              locale={locale}
              t={t}
              onPatch={onPatch}
            />
          ))}
      </div>
    </div>
  );
}

type Opt = { value: string; spec: CharacterSpecV2; ok: boolean };

/** Fields about the lower body or silhouette are previewed full body. */
export const bodyViewFor = (path: string[]) =>
  [
    "tail",
    "wings",
    "locomotion",
    "build",
    "height",
    "bottom",
    "shoes",
    "base",
  ].some((k) => path.includes(k))
    ? "body"
    : "avatar";

function Tiles({
  view = "avatar",
  name,
  legend,
  value,
  options,
  locale,
  onPick,
  unavailable,
}: {
  view?: "avatar" | "body";
  name: string;
  legend: string;
  value: string;
  options: Opt[];
  locale: string;
  onPick: (v: string) => void;
  unavailable?: (o: string) => string;
}) {
  return (
    <fieldset className="cv2-field">
      <legend>{legend}</legend>
      <div className="cv2-tiles">
        {options.map((o) => {
          const text = optionLabel(o.value, locale);
          return (
            <label
              key={o.value}
              className="cv2-tile"
              data-disabled={!o.ok || undefined}
            >
              <input
                type="radio"
                name={name}
                value={o.value}
                checked={value === o.value}
                disabled={!o.ok}
                aria-label={o.ok || !unavailable ? text : unavailable(text)}
                onChange={() => onPick(o.value)}
              />
              <CharacterPreview
                spec={o.spec}
                view={view}
                size={56}
                label={false}
              />
              <span>{text}</span>
            </label>
          );
        })}
      </div>
    </fieldset>
  );
}

function Swatches({
  name,
  legend,
  value,
  options,
  swatch,
  onPick,
  t,
  custom = true,
}: {
  name: string;
  legend: string;
  value: string;
  options: string[];
  swatch: (v: string) => string;
  onPick: (v: string) => void;
  t: (id: string, v?: Record<string, string | number>) => string;
  custom?: boolean;
}) {
  const hex = /^#[0-9a-f]{6}$/i.test(value) ? value : "#000000";
  return (
    <fieldset className="cv2-field">
      <legend>{legend}</legend>
      <div className="cv2-swatches">
        {options.map((o) => (
          <label key={o} className="cv2-swatch">
            <input
              type="radio"
              name={name}
              value={o}
              checked={value === o}
              aria-label={t("characters.v2.color.pick", { color: o })}
              onChange={() => onPick(o)}
            />
            <span style={{ background: swatch(o) }} />
          </label>
        ))}
        {custom && (
          <label className="cv2-swatch cv2-custom">
            <input
              type="color"
              value={hex}
              aria-label={t("characters.v2.color.custom")}
              onChange={(e) => onPick(e.target.value.toUpperCase())}
            />
          </label>
        )}
      </div>
    </fieldset>
  );
}

function FieldView({
  field,
  spec,
  uid,
  locale,
  t,
  onPatch,
}: {
  field: Field;
  spec: CharacterSpecV2;
  uid: string;
  locale: string;
  t: (id: string, v?: Record<string, string | number>) => string;
  onPatch: (patch: Record<string, unknown>) => void;
}) {
  const current = getAt(spec, field.path);
  const name = `${uid}-${field.id}`;
  const legend = label(t, field.id);
  const set = (value: unknown) => onPatch(toPatch(field.path, value));
  const options = useMemo<Opt[]>(() => {
    if (field.kind !== "tiles") return [];
    return fieldOptions(spec as never, field.path).map((value) => {
      const candidate = JSON.parse(JSON.stringify(spec)) as CharacterSpecV2;
      let o: Record<string, unknown> = candidate as never;
      field.path
        .slice(0, -1)
        .forEach((k) => (o = o[k] as Record<string, unknown>));
      o[field.path[field.path.length - 1]!] = value;
      return { value, spec: candidate, ok: validateSpecV2(candidate).valid };
    });
  }, [spec, field]);
  if (field.kind === "tiles")
    return (
      <Tiles
        view={bodyViewFor(field.path)}
        name={name}
        legend={legend}
        value={String(current)}
        options={options}
        locale={locale}
        onPick={set}
        unavailable={(option) =>
          t("characters.v2.option.unavailable", { option })
        }
      />
    );
  if (field.kind === "color")
    return (
      <Swatches
        name={name}
        legend={legend}
        value={String(current)}
        options={COLOR_PRESETS}
        swatch={(v) => v}
        onPick={set}
        t={t}
      />
    );
  if (field.kind === "skin")
    return (
      <Swatches
        name={name}
        legend={legend}
        value={String(current)}
        options={Object.keys(SKIN_TONES)}
        swatch={(v) => SKIN_TONES[v]!}
        onPick={set}
        t={t}
        custom={false}
      />
    );
  if (field.kind === "voice")
    return (
      <Tiles
        name={name}
        legend={legend}
        value={String(current)}
        options={[
          "voice-provisional-guide",
          "voice-provisional-supporter",
          "voice-provisional-companion",
        ].map((value) => ({
          value,
          spec: { ...spec, voice_key: value },
          ok: true,
        }))}
        locale={locale}
        onPick={set}
      />
    );
  if (field.kind === "pronouns")
    return (
      <Pronouns
        legend={legend}
        name={name}
        value={String(current)}
        t={t}
        onPick={set}
      />
    );
  if (field.kind === "accessories")
    return (
      <Accessories
        spec={spec}
        legend={legend}
        uid={uid}
        locale={locale}
        t={t}
        onPatch={onPatch}
      />
    );
  const traits = field.path[1] === "traits";
  return (
    <label className="cv2-field cv2-text">
      <span>{legend}</span>
      {field.multiline ? (
        <textarea
          rows={3}
          value={String(current)}
          onChange={(e) => set(e.target.value)}
        />
      ) : (
        <input
          type="text"
          value={traits ? (current as string[]).join(", ") : String(current)}
          onChange={(e) =>
            set(
              traits
                ? e.target.value
                    .split(",")
                    .map((s) => s.trim())
                    .filter(Boolean)
                : e.target.value,
            )
          }
        />
      )}
    </label>
  );
}

export const PRONOUN_PRESETS = {
  she: "she/her",
  he: "he/him",
  they: "they/them",
} as const;

/** Preset pronouns as radios; "Other" reveals a free entry. The value stays one string in the draft. */
function Pronouns({
  legend,
  name,
  value,
  t,
  onPick,
}: {
  legend: string;
  name: string;
  value: string;
  t: (id: string) => string;
  onPick: (v: string) => void;
}) {
  const preset = Object.entries(PRONOUN_PRESETS).find(
    ([, v]) => v === value,
  )?.[0];
  const [other, setOther] = useState(!preset && value !== "");
  const current = other ? "other" : value === "" ? "none" : (preset ?? "other");
  const choices = ["she", "he", "they", "other", "none"] as const;
  const pick = (id: (typeof choices)[number]) => {
    setOther(id === "other");
    if (id === "other") return;
    onPick(id === "none" ? "" : PRONOUN_PRESETS[id]);
  };
  return (
    <fieldset className="cv2-field">
      <legend>{legend}</legend>
      <div className="cv2-tiles cv2-chips">
        {choices.map((id) => (
          <label key={id} className="cv2-tile cv2-chip">
            <input
              type="radio"
              name={name}
              value={id}
              checked={current === id}
              onChange={() => pick(id)}
            />
            <span>{t(`characters.v2.pronoun.${id}`)}</span>
          </label>
        ))}
      </div>
      {current === "other" && (
        <label className="cv2-text cv2-pronoun-other">
          <span>{t("characters.v2.pronoun.other_label")}</span>
          <input
            type="text"
            maxLength={24}
            value={preset ? "" : value}
            onChange={(e) => onPick(e.target.value)}
          />
        </label>
      )}
    </fieldset>
  );
}

function Accessories({
  spec,
  legend,
  uid,
  locale,
  t,
  onPatch,
}: {
  spec: CharacterSpecV2;
  legend: string;
  uid: string;
  locale: string;
  t: (id: string, v?: Record<string, string | number>) => string;
  onPatch: (patch: Record<string, unknown>) => void;
}) {
  const kinds = fieldOptions(spec as never, ["clothes", "accessories", "kind"]);
  const list = spec.clothes.accessories;
  const set = (next: typeof list) =>
    onPatch({ clothes: { accessories: next } });
  return (
    <fieldset className="cv2-field">
      <legend>{legend}</legend>
      <div className="cv2-tiles">
        {kinds.map((kind) => {
          const on = list.some((a) => a.kind === kind);
          const candidate: CharacterSpecV2 = {
            ...spec,
            clothes: {
              ...spec.clothes,
              accessories: on ? list : [...list, { kind, color: "#E4572E" }],
            },
          };
          const text = optionLabel(kind, locale);
          return (
            <label key={kind} className="cv2-tile">
              <input
                type="checkbox"
                checked={on}
                disabled={!on && list.length >= 4}
                aria-label={text}
                onChange={() =>
                  set(
                    on
                      ? list.filter((a) => a.kind !== kind)
                      : [...list, { kind, color: "#E4572E" }],
                  )
                }
              />
              <CharacterPreview spec={candidate} size={56} label={false} />
              <span>{text}</span>
            </label>
          );
        })}
      </div>
      {list.map((a) => (
        <Swatches
          key={a.kind}
          name={`${uid}-acc-${a.kind}`}
          legend={t("characters.v2.accessory.color", {
            accessory: optionLabel(a.kind, locale),
          })}
          value={a.color}
          options={COLOR_PRESETS}
          swatch={(v) => v}
          t={t}
          onPick={(color) =>
            set(list.map((x) => (x.kind === a.kind ? { ...x, color } : x)))
          }
        />
      ))}
    </fieldset>
  );
}

const TAB_ICONS: Record<Tab, string> = {
  kind: "M12 3l2.6 5.6 6.1.7-4.5 4.2 1.2 6L12 16.6 6.6 19.5l1.2-6L3.3 9.3l6.1-.7z",
  look: "M3 12s3.5-6 9-6 9 6 9 6-3.5 6-9 6-9-6-9-6zM12 9.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5z",
  outfit: "M8 4l-5 4 3 3 2-1v10h8V10l2 1 3-3-5-4a4 4 0 0 1-8 0z",
  about: "M12 4a4 4 0 1 0 0 8 4 4 0 0 0 0-8zM4 20c0-4 3.6-6 8-6s8 2 8 6",
};

/** Icon tab bar (Kind, Look, Outfit, About) with arrow-key roving focus. */
export function CharacterTabs({
  tab,
  onTab,
}: {
  tab: Tab;
  onTab: (tab: Tab) => void;
}) {
  const t = useCharacterT();
  const refs = useRef<Record<string, HTMLButtonElement | null>>({});
  const onKey = (e: KeyboardEvent) => {
    const step = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    const flip = document.documentElement.dir === "rtl" ? -1 : 1;
    const next =
      TABS[(TABS.indexOf(tab) + step * flip + TABS.length) % TABS.length]!;
    onTab(next);
    refs.current[next]?.focus();
  };
  return (
    <div
      role="tablist"
      aria-label={t("characters.v2.tabs")}
      className="cv2-tabs"
      onKeyDown={onKey}
    >
      {TABS.map((id) => (
        <button
          key={id}
          ref={(el) => {
            refs.current[id] = el;
          }}
          type="button"
          role="tab"
          id={`cv2-tab-${id}`}
          aria-selected={tab === id}
          tabIndex={tab === id ? 0 : -1}
          onClick={() => onTab(id)}
        >
          <svg
            viewBox="0 0 24 24"
            width="20"
            height="20"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d={TAB_ICONS[id]} />
          </svg>
          <span>{t(`characters.v2.tab.${id}` as CharactersV2Id)}</span>
        </button>
      ))}
    </div>
  );
}
