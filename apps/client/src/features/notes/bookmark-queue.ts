export type QueuedBookmark = {
  id?: string;
  actionKey: string;
  courseId?: string;
  courseTitle?: string;
  lessonId: string;
  lessonTitle?: string;
  slideId?: number;
  slideTitle?: string;
  targetType: "slide" | "lesson";
  status: "device_saved" | "synced" | "removal_pending";
  available?: boolean;
  createdAt: string;
};

const QUEUE_STORAGE_KEY = "duolify.bookmarks.queue";

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

function loadQueue(): QueuedBookmark[] {
  try {
    const raw = localStorage.getItem(QUEUE_STORAGE_KEY);
    if (!raw) return [];
    return JSON.parse(raw) as QueuedBookmark[];
  } catch {
    return [];
  }
}

function persistQueue(queue: QueuedBookmark[]): void {
  try {
    localStorage.setItem(QUEUE_STORAGE_KEY, JSON.stringify(queue));
  } catch {
    // Storage might be unavailable
  }
}

export const bookmarkQueue = {
  getQueue(): QueuedBookmark[] {
    return loadQueue();
  },

  enqueue(item: {
    id?: string;
    actionKey?: string;
    courseId?: string;
    courseTitle?: string;
    lessonId: string;
    lessonTitle?: string;
    slideId?: number;
    slideTitle?: string;
    targetType: "slide" | "lesson";
    status?: "device_saved" | "synced" | "removal_pending";
    available?: boolean;
  }): QueuedBookmark {
    const queue = loadQueue();
    const actionKey = item.actionKey ?? generateUUID();

    // Check if target already exists in queue (deduplicate by target)
    const existingIndex = queue.findIndex(
      (b) =>
        b.lessonId === item.lessonId &&
        (b.slideId ?? null) === (item.slideId ?? null) &&
        b.status !== "removal_pending",
    );

    const queuedItem: QueuedBookmark = {
      id: item.id ?? (existingIndex >= 0 ? queue[existingIndex].id : undefined),
      actionKey:
        existingIndex >= 0 ? queue[existingIndex].actionKey : actionKey,
      courseId: item.courseId,
      courseTitle: item.courseTitle,
      lessonId: item.lessonId,
      lessonTitle: item.lessonTitle,
      slideId: item.slideId,
      slideTitle: item.slideTitle,
      targetType: item.targetType,
      status: item.status ?? "device_saved",
      available: item.available ?? true,
      createdAt:
        existingIndex >= 0
          ? queue[existingIndex].createdAt
          : new Date().toISOString(),
    };

    if (existingIndex >= 0) {
      queue[existingIndex] = { ...queue[existingIndex], ...queuedItem };
    } else {
      queue.unshift(queuedItem);
    }

    persistQueue(queue);
    return queuedItem;
  },

  enqueueRemoval(target: {
    id?: string;
    lessonId?: string;
    slideId?: number;
  }): void {
    const queue = loadQueue();
    const index = queue.findIndex((b) => {
      if (target.id && b.id === target.id) return true;
      if (
        target.lessonId &&
        b.lessonId === target.lessonId &&
        (target.slideId ?? null) === (b.slideId ?? null)
      ) {
        return true;
      }
      return false;
    });

    if (index >= 0) {
      queue[index].status = "removal_pending";
      persistQueue(queue);
    } else {
      queue.unshift({
        id: target.id,
        actionKey: generateUUID(),
        lessonId: target.lessonId || "",
        slideId: target.slideId,
        targetType: target.slideId !== undefined ? "slide" : "lesson",
        status: "removal_pending",
        createdAt: new Date().toISOString(),
      });
      persistQueue(queue);
    }
  },

  markSynced(actionKey: string, serverId?: string): void {
    const queue = loadQueue();
    const index = queue.findIndex((b) => b.actionKey === actionKey);
    if (index >= 0) {
      queue[index].status = "synced";
      if (serverId) queue[index].id = serverId;
      persistQueue(queue);
    }
  },

  remove(actionKey: string): void {
    const queue = loadQueue();
    const filtered = queue.filter((b) => b.actionKey !== actionKey);
    persistQueue(filtered);
  },

  clear(): void {
    try {
      localStorage.removeItem(QUEUE_STORAGE_KEY);
    } catch {
      // Ignore
    }
  },
};
