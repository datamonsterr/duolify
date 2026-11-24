import { session } from "../accounts/session";
import {
  anchorToWire,
  noteQueue,
  type NoteAnchor,
  type NoteSource,
  type QueuedNote,
} from "./note-queue";

export type NoteContext = {
  source: NoteSource;
  courseId?: string;
  lessonId?: string;
  slideId?: number;
  slideTitle?: string;
  /** Pins the note to a moment (and region) of the Lesson. A synced anchored note is not reopened. */
  anchor?: NoteAnchor;
};

export type NoteSyncFn = (note: {
  actionKey: string;
  courseId?: string;
  lessonId?: string;
  slideId?: number;
  slideTitle?: string;
  source: NoteSource;
  anchor?: NoteAnchor;
  rawText: string;
}) => Promise<SyncedNote>;

/** What the API says about a saved note. Voice notes also carry the free-format state. */
export type SyncedNote = {
  id: string;
  status: string;
  free_formats_left?: number;
  format_offer?: boolean;
};

export type SavedNote = {
  actionKey: string;
  id?: string;
  freeFormatsLeft?: number;
  /** The note was saved raw and the Learner may format it on request. */
  formatOffer?: boolean;
};

/**
 * The one save path for a Quick note: device queue first (stable action key), then the API when
 * online. A failed sync keeps the queued note for later. `voice` marks the anchor `input: voice`.
 */
export async function saveNote({
  context,
  body,
  voice = false,
  online,
  onSync,
  onQueued,
}: {
  context: NoteContext;
  body: string;
  voice?: boolean;
  online: boolean;
  onSync?: NoteSyncFn;
  /** Called once the note is on the device, before any network call. */
  onQueued?: (queued: QueuedNote) => void;
}): Promise<SavedNote & { synced: boolean }> {
  const anchor: NoteAnchor | undefined =
    voice && context.anchor
      ? { ...context.anchor, input: "voice" }
      : context.anchor;
  const queued = noteQueue.enqueue({
    courseId: context.courseId,
    lessonId: context.lessonId,
    slideId: context.slideId,
    slideTitle: context.slideTitle,
    source: context.source,
    anchor,
    rawText: body,
    status: "device_saved",
  });
  onQueued?.(queued);
  if (!online) return { actionKey: queued.actionKey, synced: false };
  try {
    const note = {
      actionKey: queued.actionKey,
      courseId: context.courseId,
      lessonId: context.lessonId,
      slideId: context.slideId,
      slideTitle: context.slideTitle,
      source: context.source,
      anchor,
      rawText: body,
    };
    const res = onSync
      ? await onSync(note)
      : await session.request<SyncedNote>("/v1/notes", "POST", {
          action_key: queued.actionKey,
          course_id: context.courseId,
          lesson_id: context.lessonId,
          slide_id: context.slideId,
          slide_title: context.slideTitle,
          source: context.source,
          anchor: anchorToWire(anchor),
          raw_text: body,
        });
    noteQueue.markSynced(queued.actionKey, res.id);
    return {
      actionKey: queued.actionKey,
      id: res.id,
      synced: true,
      freeFormatsLeft: res.free_formats_left,
      formatOffer: res.format_offer,
    };
  } catch {
    return { actionKey: queued.actionKey, synced: false };
  }
}

/** Undo: drop the device copy and, when it already synced, the server note. */
export async function undoSavedNote({ actionKey, id }: SavedNote) {
  noteQueue.remove(actionKey);
  if (!id) return;
  try {
    await session.request(`/v1/notes/${id}`, "DELETE");
  } catch {
    /* The note stays in Notes and can be deleted there. */
  }
}
