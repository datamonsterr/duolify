import { session } from "../accounts/session";
import type { CharacterSpec } from "./types";
import type { CharacterSpecV2 } from "./types-v2";

export type CharacterRecord = {
  id: string;
  version: number;
  spec: CharacterSpec;
  /** v2 reading of the stored document (upgraded for v1). */
  document?: CharacterSpecV2;
  read_only?: boolean;
  checksum?: string;
};
export type CastSelection = {
  guide: string;
  supporting: string[];
  version?: number;
  published?: boolean;
};
export const characterLibrary = {
  list: () => session.request<CharacterRecord[]>("/v1/characters"),
  edit: (record: CharacterRecord, spec: CharacterSpec) =>
    session.request<CharacterRecord>(`/v1/characters/${record.id}`, "PATCH", {
      expected_version: record.version,
      spec,
    }),
  createV2: (spec: CharacterSpecV2) =>
    session.request<CharacterRecord>("/v1/characters", "POST", { spec }),
  editV2: (record: CharacterRecord, spec: CharacterSpecV2) =>
    session.request<CharacterRecord>(`/v1/characters/${record.id}`, "PATCH", {
      expected_version: record.version,
      spec,
    }),
  delete: (id: string) => session.request(`/v1/characters/${id}`, "DELETE"),
  cast: (course: string) =>
    session.request<CastSelection>(`/v1/courses/${course}/cast`),
  select: (course: string, selection: CastSelection) =>
    session.request<CastSelection>(`/v1/courses/${course}/cast`, "PUT", {
      guide: selection.guide,
      supporting: selection.supporting,
    }),
};
