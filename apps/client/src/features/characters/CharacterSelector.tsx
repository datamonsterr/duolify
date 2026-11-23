import { useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
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
    f(["identity", "pronouns"], "text"),
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
const label = (t: (id: never) => string, id: string) =>
  t(`characters.v2.f.${id}` as never);

export interface SelectorProps {
  spec: CharacterSpecV2;
  /** RFC 7386 merge patch, the same contract the guided chat uses. */
  onPatch: (patch: Record<string, unknown>) => void;
  /** Whole-document replacement (kind and species switches reset to defaults). */
  onReplace: (next: CharacterSpecV2) => void;
  tab?: Tab;
  onTab?: (tab: Tab) => void;
}

export function CharacterSelector({
  spec,
  onPatch,
  onReplace,
  tab: controlled,
  onTab,
}: SelectorProps) {
  const {
    t: raw,
    preferences: { locale },
  } = useUiPreferences();
  const t = raw as (
    id: CharactersV2Id | never,
    v?: Record<string, string | number>,
  ) => string;
  const [inner, setInner] = useState<Tab>("kind");
  const tab = controlled ?? inner;
  const setTab = (next: Tab) => (onTab ? onTab(next) : setInner(next));
  const tabRefs = useRef<Record<string, HTMLButtonElement | null>>({});
  const uid = useId();
  const onTabKey = (e: KeyboardEvent) => {
    const i = TABS.indexOf(tab);
    const step = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
    const flip = document.documentElement.dir === "rtl" ? -1 : 1;
    if (!step) return;
    e.preventDefault();
    const next = TABS[(i + step * flip + TABS.length) % TABS.length]!;
    setTab(next);
    tabRefs.current[next]?.focus();
  };
  return (
    <div className="cv2-selector">
      <div
        role="tablist"
        aria-label={t("characters.v2.tabs")}
        className="cv2-tabs"
        onKeyDown={onTabKey}
      >
        {TABS.map((id) => (
          <button
            key={id}
            ref={(el) => {
              tabRefs.current[id] = el;
            }}
            type="button"
            role="tab"
            id={`${uid}-tab-${id}`}
            aria-selected={tab === id}
            aria-controls={`${uid}-panel`}
            tabIndex={tab === id ? 0 : -1}
            onClick={() => setTab(id)}
          >
            {t(`characters.v2.tab.${id}` as CharactersV2Id)}
          </button>
        ))}
      </div>
      <div
        role="tabpanel"
        id={`${uid}-panel`}
        aria-labelledby={`${uid}-tab-${tab}`}
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
                size={64}
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
  t: (id: never, v?: Record<string, string | number>) => string;
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
              aria-label={t("characters.v2.color.pick" as never, { color: o })}
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
              aria-label={t("characters.v2.color.custom" as never)}
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
  t: (id: never, v?: Record<string, string | number>) => string;
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
          t("characters.v2.option.unavailable" as never, { option })
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
  t: (id: never, v?: Record<string, string | number>) => string;
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
              <CharacterPreview spec={candidate} size={64} label={false} />
              <span>{text}</span>
            </label>
          );
        })}
      </div>
      {list.map((a) => (
        <Swatches
          key={a.kind}
          name={`${uid}-acc-${a.kind}`}
          legend={t("characters.v2.accessory.color" as never, {
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
