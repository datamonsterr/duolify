import { session } from "../accounts/session";
import type { CharacterRecord } from "./library";
import type { CharacterSpecV2 } from "./types-v2";
export type CreateJob = {
  id: string;
  state: string;
  reason_code?: string;
  proposal?: boolean;
  quote?: {
    id: string;
    pricing_version: string;
    content_ceiling: number;
    expires_at?: string;
  };
  character?: CharacterRecord;
  draft?: CharacterSpecV2;
  target?: CharacterRecord;
};
export const characterJobRecoveryKey = () =>
  `duolify.character-job.${session.current()?.id ?? "session"}`;

export function forgetCharacterJob(id: string) {
  const key = characterJobRecoveryKey();
  if (sessionStorage.getItem(key) === id) sessionStorage.removeItem(key);
}

export const characterCreation = {
  availability: () =>
    session.request<{
      enabled: boolean;
      reason_code: string;
      proposal: boolean;
    }>("/v1/characters/creation"),
  quote: (input: {
    request_key: string;
    prompt: string;
    character_id?: string;
    expected_version?: number;
    draft_schema_version?: 2;
  }) => session.request<CreateJob>("/v1/characters/jobs", "POST", input),
  confirm: (id: string, quote: string) =>
    session.request<CreateJob>(`/v1/characters/jobs/${id}/confirm`, "POST", {
      quote_id: quote,
    }),
  get: (id: string) => session.request<CreateJob>(`/v1/characters/jobs/${id}`),
  cancel: (id: string) =>
    session.request<CreateJob>(`/v1/characters/jobs/${id}`, "DELETE"),
};
