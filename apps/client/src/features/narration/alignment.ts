// Mirrors services/api/internal/narration.ValidateOutput: the player never trusts
// alignment it has not checked against the stored narration text.
export type Alignment = {
  version: 1;
  duration_ms: number;
  characters: string[];
  start_ms: number[];
  end_ms: number[];
};
const toleranceMs = 250;
const record = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === "object" && !Array.isArray(v);
const numbers = (v: unknown, n: number): v is number[] =>
  Array.isArray(v) &&
  v.length === n &&
  v.every((x) => Number.isSafeInteger(x) && x >= 0);

export function parseAlignment(raw: unknown, text: string): Alignment | null {
  if (text.trim() === "" || !record(raw) || raw.version !== 1) return null;
  const expected = [...text];
  if (
    !Array.isArray(raw.characters) ||
    raw.characters.length !== expected.length ||
    !raw.characters.every((c, i) => c === expected[i]) ||
    !numbers(raw.start_ms, expected.length) ||
    !numbers(raw.end_ms, expected.length) ||
    !Number.isSafeInteger(raw.duration_ms) ||
    Number(raw.duration_ms) <= 0
  )
    return null;
  let previous = 0;
  for (let i = 0; i < expected.length; i++) {
    if (raw.end_ms[i] < raw.start_ms[i] || raw.start_ms[i] < previous)
      return null;
    previous = raw.start_ms[i];
  }
  const last = raw.end_ms[expected.length - 1];
  if (Math.abs(last - Number(raw.duration_ms)) > toleranceMs) return null;
  return raw as unknown as Alignment;
}

/** Characters whose start time has been reached; the first is spoken at time 0. */
export function spokenCount(a: Alignment, ms: number): number {
  let n = 0;
  while (n < a.start_ms.length && a.start_ms[n] <= ms) n++;
  return n;
}
