import { useSyncExternalStore } from "react";
import { session } from "../accounts/session";

/** Free Voice note formats left today, as the API last said. Null until known. */
let left: number | null = null;
const listeners = new Set<() => void>();

export function setFormatTurnsLeft(next: number | null) {
  if (next === left) return;
  left = next;
  listeners.forEach((l) => l());
}

/** Reads the allowance from the API; a failed read leaves the last known value. */
export async function refreshFormatTurns() {
  try {
    const turns = await session.request<{ left: number }>(
      "/v1/notes/format-turns",
    );
    setFormatTurnsLeft(turns.left);
  } catch {
    /* The badge keeps what it knew. */
  }
}

export function useFormatTurnsLeft(): number | null {
  return useSyncExternalStore(
    (cb) => {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },
    () => left,
    () => null,
  );
}

/** Asks the API to format a saved Voice note (a free turn if one is left, else the Quick note price). */
export async function requestFormat(id: string) {
  const note = await session.request<{
    free_formats_left?: number;
    status: string;
  }>(`/v1/notes/${id}/format`, "POST");
  void refreshFormatTurns();
  return note;
}
