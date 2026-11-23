import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { answer, current, restore, start, toDraft } from "./engine";
import { clearLocalDrafts, createDraftStore, mergeDrafts } from "./drafts";
import type { DraftRequest } from "./drafts";
import type { Draft } from "./engine";
import { fixtureTour } from "./scripts";

type Row = { draft: Draft; updated_at: string };
/** In-memory stand-in for the drafts API with ETag and 409 semantics. */
function fakeServer(initial?: Row) {
  const state = {
    row: initial ?? (null as Row | null),
    calls: [] as string[],
    down: false,
    n: 0,
  };
  const request: DraftRequest = async <T>(
    _path: string,
    method: string,
    body?: unknown,
    headers: Record<string, string> = {},
  ) => {
    state.calls.push(method);
    if (state.down) throw Object.assign(new Error("offline"), { status: 0 });
    if (method === "GET") {
      if (!state.row) throw Object.assign(new Error("nf"), { status: 404 });
      return { ...state.row.draft, updated_at: state.row.updated_at } as T;
    }
    if (method === "DELETE") {
      state.row = null;
      return undefined as T;
    }
    const tag = state.row ? `"${state.row.updated_at}"` : undefined;
    if (headers["If-Match"] !== tag) {
      throw Object.assign(new Error("conflict"), {
        status: 409,
        details: state.row
          ? { ...state.row.draft, updated_at: state.row.updated_at }
          : { error: "changed" },
      });
    }
    state.n += 1;
    state.row = {
      draft: body as Draft,
      updated_at: `2026-11-23T10:00:0${state.n}Z`,
    };
    return { ...(body as Draft), updated_at: state.row.updated_at } as T;
  };
  return { state, request };
}
const owner = "learner-1";
const draftOf = (steps: [string, string | string[]][]) => {
  let s = start(fixtureTour);
  for (const [id, v] of steps) s = answer(fixtureTour, s, id, v).state;
  return toDraft(fixtureTour, s)!;
};

beforeEach(() => clearLocalDrafts());
afterEach(() => clearLocalDrafts());

describe("draft store", () => {
  it("saves locally at once and to the server, then restores on a fresh device", async () => {
    const server = fakeServer();
    const store = createDraftStore({
      scriptId: "fixture.tour",
      owner,
      request: server.request,
      online: () => true,
      debounceMs: 0,
    });
    const draft = draftOf([
      ["name", "Sam"],
      ["level", "some"],
    ]);
    store.save(draft);
    await store.flush();
    expect(server.state.row?.draft.answers.name.value).toBe("Sam");
    // Another device: no local copy, server copy resumes at the first unanswered step.
    clearLocalDrafts();
    const other = createDraftStore({
      scriptId: "fixture.tour",
      owner,
      request: server.request,
      online: () => true,
    });
    const loaded = await other.load();
    const resumed = restore(fixtureTour, loaded);
    expect(current(fixtureTour, resumed.state)).toBe("goals");
  });

  it("re-evaluates branches after an edit before the draft is saved", async () => {
    let s = start(fixtureTour);
    for (const [id, v] of [
      ["name", "Sam"],
      ["level", "some"],
      ["goals", ["speak"]],
      ["remind", true],
      ["time", "07:30"],
    ] as const)
      s = answer(fixtureTour, s, id, v as never).state;
    const out = answer(fixtureTour, s, "remind", false);
    expect(out.dropped).toEqual(["time"]);
    const draft = toDraft(fixtureTour, out.state)!;
    expect(draft.answers.time).toBeUndefined();
  });

  it("queues offline saves and sends them on reconnect", async () => {
    const server = fakeServer();
    let online = false;
    const store = createDraftStore({
      scriptId: "fixture.tour",
      owner,
      request: server.request,
      online: () => online,
      debounceMs: 0,
    });
    store.save(draftOf([["name", "Sam"]]));
    await store.flush();
    expect(server.state.calls).toEqual([]);
    // The local copy already holds the answer.
    const local = createDraftStore({
      scriptId: "fixture.tour",
      owner,
      request: server.request,
      online: () => false,
    });
    expect((await local.load())?.answers.name.value).toBe("Sam");
    online = true;
    await store.flush();
    expect(server.state.row?.draft.answers.name.value).toBe("Sam");
  });

  it("keeps changes made while a request is in flight", async () => {
    const server = fakeServer();
    const store = createDraftStore({
      scriptId: "fixture.tour",
      owner,
      request: server.request,
      online: () => true,
      debounceMs: 0,
    });
    store.save(draftOf([["name", "Sam"]]));
    const first = store.flush();
    store.save(
      draftOf([
        ["name", "Sam"],
        ["level", "some"],
      ]),
    );
    await first;
    await store.flush();
    expect(server.state.row?.draft.answers.level.value).toBe("some");
  });

  it("merges per step after a conflict from another device", async () => {
    const server = fakeServer();
    const a = createDraftStore({
      scriptId: "fixture.tour",
      owner,
      request: server.request,
      online: () => true,
      debounceMs: 0,
    });
    a.save(draftOf([["name", "Sam"]]));
    await a.flush();
    // Device B saves a different step; device A, holding an old tag, edits and syncs.
    server.state.row = {
      draft: draftOf([
        ["name", "Sam"],
        ["level", "expert"],
      ]),
      updated_at: "2026-11-23T11:00:00Z",
    };
    a.save(draftOf([["name", "Alex"]]));
    await a.flush();
    const merged = server.state.row!.draft.answers;
    expect(merged.name.value).toBe("Alex");
    expect(merged.level.value).toBe("expert");
  });

  it("mergeDrafts prefers this device's answers and keeps server-only steps", () => {
    const merged = mergeDrafts(
      draftOf([["name", "Mine"]]),
      draftOf([
        ["name", "Theirs"],
        ["level", "some"],
      ]),
    );
    expect(merged.answers.name.value).toBe("Mine");
    expect(merged.answers.level.value).toBe("some");
  });

  it("drops a stale clean local copy when the server draft expired, keeps a dirty one", async () => {
    const server = fakeServer();
    const store = createDraftStore({
      scriptId: "fixture.tour",
      owner,
      request: server.request,
      online: () => true,
      debounceMs: 0,
    });
    store.save(draftOf([["name", "Sam"]]));
    await store.flush();
    server.state.row = null; // purged by the worker
    expect(await store.load()).toBeNull();
    store.save(draftOf([["name", "Kept"]]));
    server.state.down = true;
    await store.flush();
    server.state.down = false;
    server.state.row = null;
    expect((await store.load())?.answers.name.value).toBe("Kept");
  });

  it("clears the local copy and the server draft on completion and on sign-out", async () => {
    const server = fakeServer();
    const store = createDraftStore({
      scriptId: "fixture.tour",
      owner,
      request: server.request,
      online: () => true,
      debounceMs: 0,
    });
    store.save(draftOf([["name", "Sam"]]));
    await store.flush();
    await store.clear();
    expect(server.state.row).toBeNull();
    store.save(draftOf([["name", "Again"]]));
    clearLocalDrafts();
    const fresh = createDraftStore({
      scriptId: "fixture.tour",
      owner,
      request: server.request,
      online: () => false,
    });
    expect(await fresh.load()).toBeNull();
  });
});
