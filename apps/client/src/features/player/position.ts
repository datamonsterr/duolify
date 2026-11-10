import { session } from "../accounts/session";
import type { Manifest } from "./manifest";
export type Position = { version: number; slide_id: string; time_ms: number };
export function validPosition(
  value: unknown,
  manifest: Manifest,
): value is Position {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const p = value as Position;
  const slide = manifest.slides.find((s) => s.slide_id === p.slide_id);
  return (
    Object.keys(value).length === 3 &&
    p.version === manifest.version &&
    !!slide &&
    Number.isSafeInteger(p.time_ms) &&
    p.time_ms >= 0 &&
    p.time_ms <= slide.duration_ms
  );
}
export function playerReturnPath(raw: unknown, course: string) {
  return typeof raw === "string" &&
    /^(\/|\/courses|\/notes|\/bookmarks|\/course\/[A-Za-z0-9_-]+)$/.test(raw)
    ? raw
    : `/course/${course}`;
}
const key = (owner: string, lesson: string) =>
  `player:position:${owner}:${lesson}`;
export function readLocalPosition(
  owner: string,
  manifest: Manifest,
): Position | null {
  try {
    const value: unknown = JSON.parse(
      sessionStorage.getItem(key(owner, manifest.lesson_id)) ?? "null",
    );
    return validPosition(value, manifest) ? value : null;
  } catch {
    return null;
  }
}
export function writeLocalPosition(
  owner: string,
  lesson: string,
  value: Position,
) {
  try {
    sessionStorage.setItem(key(owner, lesson), JSON.stringify(value));
  } catch {
    /* Postgres remains authoritative. */
  }
}
export async function loadPosition(manifest: Manifest) {
  const value = await session.request<unknown>(
    `/v1/lessons/${manifest.lesson_id}/position`,
  );
  if (value !== null && !validPosition(value, manifest))
    throw new Error("position_unavailable");
  return value as Position | null;
}
// Serialize across slide mounts: a late old-slide write cannot overwrite Next.
let writing: Promise<unknown> = Promise.resolve();
export function savePosition(owner: string, lesson: string, value: Position) {
  // Unmount cleanup after logout must never recreate a cleared local cursor.
  if (session.current()?.id !== owner)
    return Promise.reject(new Error("session_changed"));
  writeLocalPosition(owner, lesson, value);
  const operation = writing
    .catch(() => {})
    .then(async () => {
      if (session.current()?.id !== owner) throw new Error("session_changed");
      const saved = await session.request<Position>(
        `/v1/lessons/${lesson}/position`,
        "PUT",
        value,
      );
      if (
        !saved ||
        Object.keys(saved).length !== 3 ||
        saved.version !== value.version ||
        saved.slide_id !== value.slide_id ||
        saved.time_ms !== value.time_ms
      )
        throw new Error("position_unavailable");
      return saved;
    });
  writing = operation;
  return operation;
}
