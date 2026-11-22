import { AuthError, session } from "../accounts/session";
import { mergeState, type ProgressState } from "./tour";
export type Progress = {
  welcome_state: ProgressState;
  tour_state: ProgressState;
};
type Field = keyof Progress;
type Local = Progress & { dirty: Field[] };
const empty: Progress = { welcome_state: "pending", tour_state: "pending" };
const memory = new Map<string, Local>();
const storageKey = (owner: string) => `duolify.onboarding.v1:${owner}`;
const valid = (state: unknown): state is ProgressState =>
  ["pending", "skipped", "completed"].includes(String(state));
function readLocal(owner: string): Local | undefined {
  try {
    const data = JSON.parse(
      localStorage.getItem(storageKey(owner)) ?? "null",
    ) as Local | null;
    if (
      data &&
      valid(data.welcome_state) &&
      valid(data.tour_state) &&
      Array.isArray(data.dirty)
    )
      return {
        ...data,
        dirty: data.dirty.filter(
          (field) => field === "welcome_state" || field === "tour_state",
        ),
      };
  } catch {
    /* Memory fallback also covers denied storage. */
  }
  return memory.get(owner);
}
function writeLocal(owner: string, value: Local) {
  memory.set(owner, value);
  try {
    localStorage.setItem(storageKey(owner), JSON.stringify(value));
  } catch {
    /* Navigation remains available. */
  }
}
// Serialize per-learner writes. Re-read after each response so an in-flight
// acknowledgement cannot erase a newer local completion or its dirty flag.
const syncing = new Map<string, Promise<Progress>>();
export async function loadProgress(owner: string): Promise<Progress | null> {
  try {
    return await syncProgress(owner);
  } catch {
    return readLocal(owner) ?? null;
  }
}
export function acknowledge(
  owner: string,
  field: Field,
  state: "completed" | "skipped",
) {
  const current = readLocal(owner) ?? { ...empty, dirty: [] };
  writeLocal(owner, {
    ...current,
    [field]: mergeState(current[field], state),
    dirty: [...new Set([...current.dirty, field])],
  });
  void syncProgress(owner).catch(() => {});
}
async function syncProgress(owner: string): Promise<Progress> {
  const active = syncing.get(owner);
  if (active) {
    await active;
    return syncProgress(owner);
  }
  const task = (async () => {
    if (session.current()?.id !== owner) throw Error("Account changed");
    const local = readLocal(owner);
    const patch = Object.fromEntries(
      (local?.dirty ?? [])
        .filter((field) => local?.[field] !== "pending")
        .map((field) => [field, local![field]]),
    );
    const remote = await session.request<Progress>(
      "/v1/me/onboarding",
      Object.keys(patch).length ? "PATCH" : "GET",
      Object.keys(patch).length ? patch : undefined,
    );
    if (!valid(remote.welcome_state) || !valid(remote.tour_state))
      throw Error("Invalid progress");
    const latest = readLocal(owner);
    const merged: Local = {
      welcome_state: mergeState(
        remote.welcome_state,
        latest?.welcome_state ?? "pending",
      ),
      tour_state: mergeState(
        remote.tour_state,
        latest?.tour_state ?? "pending",
      ),
      dirty: (latest?.dirty ?? []).filter(
        (field) => latest?.[field] !== remote[field],
      ),
    };
    writeLocal(owner, merged);
    return merged;
  })();
  syncing.set(owner, task);
  try {
    return await task;
  } finally {
    syncing.delete(owner);
  }
}
export type Example = {
  slug: string;
  title: string;
  description: string;
  level: string;
  units: number;
  lessons: number;
  estimated_minutes: number;
};
export async function listExamples(): Promise<Example[]> {
  try {
    const { examples } = await session.request<{ examples: Example[] }>(
      "/v1/courses/examples",
    );
    return examples.filter((value) => /^[a-z0-9-]+$/.test(value.slug));
  } catch (error) {
    if (error instanceof AuthError && error.status === 404) return [];
    return []; // Other paths remain useful while the catalogue is unavailable.
  }
}
export const enrollExample = (slug: string) =>
  session.request<{ course_id: string }>(
    `/v1/courses/examples/${encodeURIComponent(slug)}/enroll`,
    "POST",
  );
