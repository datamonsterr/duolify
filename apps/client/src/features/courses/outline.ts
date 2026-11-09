import { AuthError, session } from "../accounts/session";
import type { Outline, OutlineSnapshot } from "../design/chat/OutlineEditor";

export type ConfirmedOutline = {
  draft_id: string;
  revision: number;
  content_hash: string;
  outline: Outline;
};
export type OutlineDraft = {
  id: string;
  brief_revision: string;
  revision: number;
  outline: Outline;
  history: OutlineSnapshot[];
  confirmed: ConfirmedOutline | null;
};
export type OutlineConflict = { current: OutlineDraft; recovery: OutlineDraft };
type WorkingOutline = {
  value: Outline;
  history: OutlineSnapshot[];
  draft?: OutlineDraft;
};
function workingKey(briefRevision: string) {
  const owner = session.current()?.id;
  return owner ? `course-outline:${owner}:${briefRevision}` : undefined;
}
function outlineShape(value: unknown): value is Outline {
  return (
    Array.isArray(value) &&
    value.every(
      (unit) =>
        unit &&
        typeof unit.id === "string" &&
        typeof unit.title === "string" &&
        Array.isArray(unit.lessons) &&
        unit.lessons.every(
          (lesson: { id?: unknown; title?: unknown; goal?: unknown }) =>
            lesson &&
            typeof lesson.id === "string" &&
            typeof lesson.title === "string" &&
            typeof lesson.goal === "string",
        ),
    )
  );
}
// Disposable account-scoped recovery only; Postgres is authoritative after sync.
export function readWorkingOutline(
  briefRevision: string,
): WorkingOutline | undefined {
  try {
    const key = workingKey(briefRevision);
    const value = key
      ? JSON.parse(sessionStorage.getItem(key) || "null")
      : null;
    if (
      value &&
      outlineShape(value.value) &&
      Array.isArray(value.history) &&
      value.history.length <= 50 &&
      value.history.every(
        (entry: OutlineSnapshot) =>
          entry &&
          outlineShape(entry.value) &&
          typeof entry.focusId === "string",
      ) &&
      (!value.draft ||
        (value.draft.brief_revision === briefRevision &&
          typeof value.draft.id === "string" &&
          Number.isSafeInteger(value.draft.revision) &&
          value.draft.revision > 0))
    )
      return value;
  } catch {
    /* Invalid or unavailable cache never blocks the saved outline. */
  }
}
export function writeWorkingOutline(
  briefRevision: string,
  value?: WorkingOutline,
) {
  try {
    const key = workingKey(briefRevision);
    if (key) {
      if (value) sessionStorage.setItem(key, JSON.stringify(value));
      else sessionStorage.removeItem(key);
    }
  } catch {
    /* The mounted editor still keeps unsaved work. */
  }
}
export interface OutlineService {
  latest(briefRevision: string): Promise<OutlineDraft | null>;
  create(
    briefRevision: string,
    outline: Outline,
    history: OutlineSnapshot[],
  ): Promise<OutlineDraft>;
  save(
    draft: OutlineDraft,
    outline: Outline,
    history: OutlineSnapshot[],
  ): Promise<OutlineDraft>;
  confirm(draft: OutlineDraft): Promise<ConfirmedOutline>;
}
export function conflictFrom(error: unknown): OutlineConflict | undefined {
  if (
    error instanceof AuthError &&
    error.status === 409 &&
    error.details &&
    typeof error.details === "object" &&
    "current" in error.details &&
    "recovery" in error.details
  )
    return error.details as OutlineConflict;
}
export const outlineApi: OutlineService = {
  latest: async (briefRevision) =>
    (
      await session.request<{ draft: OutlineDraft | null }>(
        `/v1/outline-drafts?brief_revision=${encodeURIComponent(briefRevision)}`,
      )
    ).draft,
  create: (briefRevision, outline, history) =>
    session.request("/v1/outline-drafts", "POST", {
      brief_revision: briefRevision,
      outline,
      history,
    }),
  save: (draft, outline, history) =>
    session.request(
      `/v1/outline-drafts/${encodeURIComponent(draft.id)}`,
      "PUT",
      { expected_revision: draft.revision, outline, history },
    ),
  confirm: (draft) =>
    session.request(
      `/v1/outline-drafts/${encodeURIComponent(draft.id)}/confirm`,
      "POST",
      { expected_revision: draft.revision },
    ),
};

export function sampleOutline(
  units: { title: string; lessons: { title: string; goal: string }[] }[],
): Outline {
  return units.map((unit) => ({
    id: crypto.randomUUID(),
    title: unit.title,
    lessons: unit.lessons.map((lesson) => ({
      ...lesson,
      id: crypto.randomUUID(),
    })),
  }));
}

export function validOutline(outline: Outline, confirm = false) {
  return (
    outline.length <= 10 &&
    (!confirm || outline.length > 0) &&
    outline.every(
      (unit) =>
        unit.title.trim() &&
        unit.lessons.length <= 20 &&
        (!confirm || unit.lessons.length > 0) &&
        unit.lessons.every(
          (lesson) => lesson.title.trim() && lesson.goal?.trim(),
        ),
    )
  );
}
