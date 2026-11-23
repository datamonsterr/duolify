import { useUiPreferences } from "../design/localization/Preferences";
import { charactersV2Ar, charactersV2En } from "./messages";

/** Creator strings live in their own catalogue so the first-load bundle stays small. */
export function useCharacterT() {
  const {
    preferences: { locale },
  } = useUiPreferences();
  const table: Record<string, string> =
    locale === "ar" ? charactersV2Ar : charactersV2En;
  return (id: string, vars?: Record<string, string | number>) =>
    (
      table[id] ??
      charactersV2En[id as keyof typeof charactersV2En] ??
      id
    ).replace(/\{(\w+)\}/g, (_, k: string) => String(vars?.[k] ?? ""));
}
