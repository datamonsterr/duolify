import { session } from "../accounts/session";
import type { Position } from "./position";

export type PlaybackSyncEvent = {
  action_key: string;
  lesson_id: string;
  version: number;
  slide_id: string;
  time_ms: number;
  occurred_at: string;
};

const prefix = "player:sync-queue:";
const queueKey = (owner: string, lessonId: string) =>
  `${prefix}${owner}:${lessonId}`;

const listeners = new Set<() => void>();

function notify() {
  listeners.forEach((listener) => {
    try {
      listener();
    } catch {
      /* Listeners must not fail sync operations. */
    }
  });
}

export function subscribeSyncStatus(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function generateActionKey(): string {
  if (
    typeof crypto !== "undefined" &&
    typeof crypto.randomUUID === "function"
  ) {
    return crypto.randomUUID();
  }
  return (
    "act-" +
    Math.random().toString(36).slice(2, 11) +
    "-" +
    Date.now().toString(36)
  );
}

export function readSyncQueue(
  owner: string,
  lessonId?: string,
): PlaybackSyncEvent[] {
  if (!owner) return [];
  try {
    if (lessonId) {
      const raw = localStorage.getItem(queueKey(owner, lessonId));
      if (!raw) return [];
      const parsed = JSON.parse(raw);
      return Array.isArray(parsed) ? parsed : [];
    }
    const all: PlaybackSyncEvent[] = [];
    const ownerPrefix = `${prefix}${owner}:`;
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k && k.startsWith(ownerPrefix)) {
        const raw = localStorage.getItem(k);
        if (raw) {
          const parsed = JSON.parse(raw);
          if (Array.isArray(parsed)) all.push(...parsed);
        }
      }
    }
    return all;
  } catch {
    return [];
  }
}

export function enqueueSyncEvent(
  owner: string,
  event: PlaybackSyncEvent,
): void {
  if (!owner || !event.lesson_id || !event.action_key) return;
  try {
    const k = queueKey(owner, event.lesson_id);
    const existing = readSyncQueue(owner, event.lesson_id);
    if (existing.some((e) => e.action_key === event.action_key)) return;
    existing.push(event);
    localStorage.setItem(k, JSON.stringify(existing));
    notify();
  } catch {
    /* Device storage failure preserves application operation. */
  }
}

export function removeSyncEvents(
  owner: string,
  lessonId: string,
  actionKeys: string[],
): void {
  if (!owner || !lessonId || actionKeys.length === 0) return;
  try {
    const k = queueKey(owner, lessonId);
    const existing = readSyncQueue(owner, lessonId);
    const keysToRemove = new Set(actionKeys);
    const remaining = existing.filter((e) => !keysToRemove.has(e.action_key));
    if (remaining.length === 0) {
      localStorage.removeItem(k);
    } else {
      localStorage.setItem(k, JSON.stringify(remaining));
    }
    notify();
  } catch {
    /* Device storage cleanup failures are ignored. */
  }
}

export function clearSyncQueue(owner?: string): void {
  try {
    const ownerPrefix = owner ? `${prefix}${owner}:` : prefix;
    const toRemove: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k && k.startsWith(ownerPrefix)) {
        toRemove.push(k);
      }
    }
    toRemove.forEach((k) => localStorage.removeItem(k));
    notify();
  } catch {
    /* Storage clear failure is non-fatal. */
  }
}

export function hasPendingSync(owner: string, lessonId: string): boolean {
  return readSyncQueue(owner, lessonId).length > 0;
}

// Serialize synchronization operations across parallel triggers
let syncingPromise: Promise<Position | null> = Promise.resolve(null);

export function synchronizeProgress(
  owner: string,
  lessonId: string,
): Promise<Position | null> {
  const queued = readSyncQueue(owner, lessonId);
  if (queued.length === 0) {
    return Promise.resolve(null);
  }

  const syncOp = syncingPromise
    .catch(() => null)
    .then(async () => {
      // Re-read queue in case previous execution cleared it
      const currentQueue = readSyncQueue(owner, lessonId);
      if (currentQueue.length === 0) return null;

      const body = { events: currentQueue };
      const response = await session.request<{
        synced: boolean;
        action_keys: string[];
        position: Position;
      }>(`/v1/lessons/${lessonId}/sync`, "POST", body);

      if (response && response.synced && Array.isArray(response.action_keys)) {
        removeSyncEvents(owner, lessonId, response.action_keys);
      }

      return response?.position ?? null;
    });

  syncingPromise = syncOp;
  return syncOp;
}
