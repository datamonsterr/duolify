export const usernameRules = { min: 3, max: 20 };
export const usernameHelp =
  "3–20 letters, numbers, _ or . · start with a letter · your capitalization is kept";
export const invalidUsername =
  "Use 3–20 characters: letters, numbers, _ or . — start with a letter.";
const shape = /^[A-Za-z][A-Za-z0-9_.]*$/;

/** Mirrors services/api/internal/social: NFKC, 3-20 chars, no trailing or doubled separators. */
export function validateUsername(
  raw: string,
  rules = usernameRules,
): string | null {
  const name = normalizeUsername(raw);
  const length = [...name].length;
  if (
    length < rules.min ||
    length > rules.max ||
    !shape.test(name) ||
    /[_.]$/.test(name) ||
    /[_.]{2}/.test(name)
  )
    return invalidUsername;
  return null;
}
export function normalizeUsername(raw: string): string {
  return raw.trim().normalize("NFKC");
}
