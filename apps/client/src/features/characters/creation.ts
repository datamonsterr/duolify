import { session } from "../accounts/session";
import type { CharacterRecord } from "./library";
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
};
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
  }) => session.request<CreateJob>("/v1/characters/jobs", "POST", input),
  confirm: (id: string, quote: string) =>
    session.request<CreateJob>(`/v1/characters/jobs/${id}/confirm`, "POST", {
      quote_id: quote,
    }),
  get: (id: string) => session.request<CreateJob>(`/v1/characters/jobs/${id}`),
  cancel: (id: string) =>
    session.request<CreateJob>(`/v1/characters/jobs/${id}`, "DELETE"),
};
