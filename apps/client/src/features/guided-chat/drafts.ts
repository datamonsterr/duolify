import type { Draft } from "./engine";
import type { Answers } from "./types";

/** Transport for the drafts API. `session.request` fits; tests pass a fake. */
export type DraftRequest = <T>(
  path: string,
  method: "GET" | "PUT" | "DELETE",
  body?: unknown,
  headers?: Record<string, string>,
) => Promise<T>;

type Stored = {
  draft: Draft | null;
  /** ETag of the last server copy this device saw; sent as If-Match. */
  etag: string | null;
  /** True while the local copy has changes the server has not accepted. */
  dirty: boolean;
};

export type DraftStore = {
  /** Local copy first (instant resume), then the server copy merged per step. */
  load(): Promise<Draft | null>;
  /** Writes the local copy at once and syncs in the background. null clears. */
  save(draft: Draft | null): void;
  /** Completion: drop the local copy and the server draft. */
  clear(): Promise<void>;
  /** Pushes pending changes now (e.g. on reconnect). */
  flush(): Promise<void>;
  dispose(): void;
};

const prefix = "duolify.guided-draft.v1:";
const memory = new Map<string, Stored>();

/** Per-step merge after a conflict: the newer side wins for steps both hold, the union is kept. */
export function mergeDrafts(mine: Draft, server: Draft): Draft {
  const answers: Answers = { ...server.answers, ...mine.answers };
  return { ...mine, answers };
}

function read(key: string): Stored | null {
  try {
    const raw = localStorage.getItem(key);
    if (raw) return JSON.parse(raw) as Stored;
  } catch {
    /* denied or corrupt storage: fall back to memory */
  }
  return memory.get(key) ?? null;
}
function write(key: string, value: Stored | null) {
  if (value) memory.set(key, value);
  else memory.delete(key);
  try {
    if (value) localStorage.setItem(key, JSON.stringify(value));
    else localStorage.removeItem(key);
  } catch {
    /* memory copy still works */
  }
}

/** Removes every local draft; call on sign-out. */
export function clearLocalDrafts() {
  memory.clear();
  try {
    for (const key of Object.keys(localStorage))
      if (key.startsWith(prefix)) localStorage.removeItem(key);
  } catch {
    /* nothing to clear */
  }
}

type Wire = Draft & { updated_at: string };
const etagOf = (updatedAt: string | undefined) =>
  updatedAt ? `"${updatedAt}"` : null;
const status = (error: unknown) =>
  typeof error === "object" && error && "status" in error
    ? Number((error as { status: unknown }).status)
    : 0;
const details = (error: unknown) =>
  (error as { details?: unknown } | null)?.details as Wire | undefined;

export function createDraftStore({
  scriptId,
  owner,
  request,
  online = () => navigator.onLine,
  debounceMs = 400,
}: {
  scriptId: string;
  /** Learner id; keeps two accounts on one device apart. */
  owner: string;
  request: DraftRequest;
  online?: () => boolean;
  debounceMs?: number;
}): DraftStore {
  const key = `${prefix}${owner}:${scriptId}`;
  const path = `/v1/me/guided-drafts/${scriptId}`;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let running: Promise<void> | null = null;
  const onOnline = () => void flush();

  async function push(): Promise<void> {
    for (let attempt = 0; attempt < 2; attempt += 1) {
      const local = read(key);
      if (!local?.dirty || !online()) return;
      try {
        if (local.draft === null) {
          await request(path, "DELETE");
          write(key, null);
          return;
        }
        const { updated_at: _ignored, ...body } = local.draft;
        void _ignored;
        const saved = await request<Wire>(
          path,
          "PUT",
          body,
          local.etag ? { "If-Match": local.etag } : {},
        );
        const now = read(key);
        // A change made while the request was in flight stays dirty for the next push.
        const same =
          JSON.stringify(now?.draft?.answers) ===
          JSON.stringify(local.draft.answers);
        write(key, {
          draft: same
            ? { ...local.draft, updated_at: saved.updated_at }
            : (now?.draft ?? null),
          etag: etagOf(saved.updated_at),
          dirty: !same,
        });
        if (same) return;
      } catch (error) {
        if (status(error) !== 409) return; // offline or server trouble: stay dirty, retry later
        const theirs = details(error);
        write(key, {
          draft:
            theirs?.answers && local.draft
              ? mergeDrafts(local.draft, theirs)
              : local.draft,
          etag: etagOf(theirs?.updated_at),
          dirty: true,
        });
      }
    }
  }
  function flush() {
    clearTimeout(timer);
    running = (running ?? Promise.resolve()).then(push, push);
    return running;
  }
  if (typeof window !== "undefined")
    window.addEventListener("online", onOnline);

  return {
    async load() {
      const local = read(key);
      if (!online()) return local?.draft ?? null;
      try {
        const server = await request<Wire>(path, "GET");
        const etag = etagOf(server.updated_at);
        if (local?.dirty && local.draft) {
          const merged = mergeDrafts(local.draft, server);
          write(key, { draft: merged, etag, dirty: true });
          void flush();
          return merged;
        }
        write(key, { draft: server, etag, dirty: false });
        return server;
      } catch (error) {
        if (status(error) === 404) {
          // Expired or cleared elsewhere: a clean local copy is stale, a dirty one is still ours.
          if (local && !local.dirty) write(key, null);
          return local?.dirty ? local.draft : null;
        }
        return local?.draft ?? null;
      }
    },
    save(draft) {
      const previous = read(key);
      write(key, { draft, etag: previous?.etag ?? null, dirty: true });
      clearTimeout(timer);
      timer = setTimeout(() => void flush(), debounceMs);
    },
    async clear() {
      clearTimeout(timer);
      write(key, { draft: null, etag: null, dirty: true });
      await flush();
      write(key, null);
    },
    flush,
    dispose() {
      clearTimeout(timer);
      if (typeof window !== "undefined")
        window.removeEventListener("online", onOnline);
    },
  };
}
