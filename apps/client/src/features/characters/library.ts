import { session } from "../accounts/session";
import type { CharacterSpec } from "./types";

export type CharacterRecord = {
  id: string;
  version: number;
  spec: CharacterSpec;
};
export type CastSelection = {
  guide: string;
  supporting: string[];
  version?: number;
};
export const characterLibrary = {
  list: () => session.request<CharacterRecord[]>("/v1/characters"),
  edit: (record: CharacterRecord, spec: CharacterSpec) =>
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
