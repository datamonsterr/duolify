import schemaV2 from "../../../../../services/api/internal/characters/schema/v2.json";
import partsJson from "../../../../../services/api/internal/characters/rig/parts.v2.json";
import speciesJson from "../../../../../services/api/internal/characters/schema/species.v2.json";
import type { BodyKind } from "./types-v2";

type SchemaNode = {
  enum?: string[];
  properties?: Record<string, SchemaNode>;
  oneOf?: SchemaNode[];
  items?: SchemaNode;
};

export const KINDS: BodyKind[] = ["human", "animal", "robot", "fantasy"];
const root = schemaV2 as unknown as SchemaNode;

/** Catalog ids for a path (body paths are resolved per kind), straight from schema/v2.json. */
export function optionsFor(kind: BodyKind, path: string[]): string[] {
  let node: SchemaNode | undefined = root;
  for (const [i, key] of path.entries()) {
    if (i === 1 && path[0] === "body") {
      node = node?.oneOf?.find(
        (b) => (b.properties?.kind as { const?: string })?.const === kind,
      );
    }
    node = node?.properties?.[key];
    if (path[0] === "clothes" && key === "accessories") node = node?.items;
  }
  if (path[0] === "body" && path.length === 1) return KINDS;
  return node?.enum ?? (node as SchemaNode | undefined)?.items?.enum ?? [];
}

export const humanize = (id: string) =>
  id.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());

const skinTable = (
  partsJson as unknown as {
    tables: { on: string; map: Record<string, Record<string, string>> }[];
  }
).tables.find((t) => t.on === "skin_tone")!;
/** Skin tone id to its swatch colour, from the part library. */
export const SKIN_TONES: Record<string, string> = Object.fromEntries(
  Object.entries(skinTable.map).map(([id, v]) => [id, v.skin!]),
);

export const COLOR_PRESETS = [
  "#2B2B2B",
  "#8A8F98",
  "#FFFFFF",
  "#E4572E",
  "#FF8A00",
  "#FFC233",
  "#58C322",
  "#1F7A0D",
  "#1677C9",
  "#7B3FE4",
  "#FF8FD0",
  "#8A5230",
  "#3A2418",
  "#C9A15B",
  "#E8A86B",
  "#9DB4C0",
];

type SpeciesTable = Record<string, Record<string, Record<string, string[]>>>;
const SPECIES = speciesJson as unknown as SpeciesTable;

/** Species ids of a kind (animal, fantasy), in schema order. */
export const speciesOf = (kind: BodyKind): string[] =>
  Object.keys(SPECIES[kind] ?? {});

/** Ears, tail, markings, wings or horns allowed for a species; the first value is its default. */
export const speciesAttr = (
  kind: BodyKind,
  species: string,
  attr: string,
): string[] => SPECIES[kind]?.[species]?.[attr] ?? [];

/** The species-controlled attributes of a kind. */
export const speciesAttrs = (kind: BodyKind, species: string): string[] =>
  Object.keys(SPECIES[kind]?.[species] ?? {});

const SPECIES_BODY_ATTRS = ["markings", "ears", "tail", "wings", "horns"];
/** Options for a body path, limited to the species-allowed list when the species controls it. */
export function fieldOptions(
  spec: { body: { kind: BodyKind } & Record<string, unknown> },
  path: string[],
): string[] {
  const { kind } = spec.body;
  const species = spec.body.species as string | undefined;
  if (
    species &&
    path[0] === "body" &&
    path.length === 2 &&
    SPECIES_BODY_ATTRS.includes(path[1]!)
  ) {
    const allowed = speciesAttr(kind, species, path[1]!);
    if (allowed.length) return allowed;
  }
  if (path[0] === "body" && path[1] === "species") return speciesOf(kind);
  return optionsFor(kind, path);
}
