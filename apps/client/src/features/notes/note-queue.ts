export type NoteSource = "slide" | "lesson" | "chat";

/** A Lesson moment a note points at: playback time, optional region (0..1 of the 16:9 stage), how it was entered. */
export type NoteAnchor = {
  playbackMs: number;
  rect?: { x: number; y: number; w: number; h: number };
  input: "typed" | "voice";
};

/** The wire shape of an anchor on `POST /v1/notes`. Serialised the same way for a live save and a queue replay. */
export function anchorToWire(anchor: NoteAnchor | undefined) {
  if (!anchor) return undefined;
  return {
    playback_ms: Math.max(0, Math.round(anchor.playbackMs)),
    rect: anchor.rect,
    input: anchor.input,
  };
}

export type QueuedNote = {
  actionKey: string;
  courseId?: string;
  lessonId?: string;
  slideId?: number;
  slideTitle?: string;
  source: NoteSource;
  anchor?: NoteAnchor;
  rawText: string;
  status: "enrichment_pending" | "device_saved" | "synced";
  createdAt: string;
  serverNoteId?: string;
};

const QUEUE_STORAGE_KEY = "duolify.notes.queue";

function generateUUID(): string {
  if (
    typeof crypto !== "undefined" &&
    typeof crypto.randomUUID === "function"
  ) {
    return crypto.randomUUID();
  }
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === "x" ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

function loadQueue(): QueuedNote[] {
  try {
    const raw = localStorage.getItem(QUEUE_STORAGE_KEY);
    if (!raw) return [];
    return JSON.parse(raw) as QueuedNote[];
  } catch {
    return [];
  }
}

function persistQueue(queue: QueuedNote[]): void {
  try {
    localStorage.setItem(QUEUE_STORAGE_KEY, JSON.stringify(queue));
  } catch {
    // Storage might be unavailable or full
  }
}

export const noteQueue = {
  getQueue(): QueuedNote[] {
    return loadQueue();
  },

  enqueue(item: {
    actionKey?: string;
    courseId?: string;
    lessonId?: string;
    slideId?: number;
    slideTitle?: string;
    source: NoteSource;
    anchor?: NoteAnchor;
    rawText: string;
    status?: "device_saved" | "enrichment_pending" | "synced";
  }): QueuedNote {
    const queue = loadQueue();
    const actionKey = item.actionKey ?? generateUUID();
    const existingIndex = queue.findIndex((n) => n.actionKey === actionKey);

    const queuedNote: QueuedNote = {
      actionKey,
      courseId: item.courseId,
      lessonId: item.lessonId,
      slideId: item.slideId,
      slideTitle: item.slideTitle,
      source: item.source,
      anchor: item.anchor,
      rawText: item.rawText,
      status: item.status ?? "device_saved",
      createdAt: new Date().toISOString(),
    };

    if (existingIndex >= 0) {
      queue[existingIndex] = { ...queue[existingIndex], ...queuedNote };
    } else {
      queue.unshift(queuedNote);
    }

    persistQueue(queue);
    return queuedNote;
  },

  update(
    actionKey: string,
    updates: Partial<QueuedNote>,
  ): QueuedNote | undefined {
    const queue = loadQueue();
    const index = queue.findIndex((n) => n.actionKey === actionKey);
    if (index === -1) return undefined;
    queue[index] = { ...queue[index], ...updates };
    persistQueue(queue);
    return queue[index];
  },

  markSynced(actionKey: string, serverNoteId?: string): void {
    this.update(actionKey, {
      status: "synced",
      serverNoteId,
    });
  },

  clear(): void {
    try {
      localStorage.removeItem(QUEUE_STORAGE_KEY);
    } catch {
      // ignore
    }
  },
};
