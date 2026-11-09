import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { trackAppearance, trackPreference } from "../../../lib/analytics";
import { createFormatter, resolveLocale, type Locale } from "./messages";
export type Preferences = {
  locale: Locale;
  appearance: "system" | "light" | "dark";
  reduceMotion: boolean;
};
export type PreferenceAdapter = {
  load: () => unknown;
  save: (value: Preferences) => void;
  clear: () => void;
};
export const defaultPreferences: Preferences = {
  locale: "en",
  appearance: "system",
  reduceMotion: false,
};
export function sanitizePreferences(value: unknown): Preferences {
  const input =
    value && typeof value === "object"
      ? (value as Record<string, unknown>)
      : {};
  return {
    locale: resolveLocale(input.locale),
    appearance:
      input.appearance === "light" || input.appearance === "dark"
        ? input.appearance
        : "system",
    reduceMotion: input.reduceMotion === true,
  };
}
export const resolveReducedMotion = (system: boolean, user: boolean) =>
  system || user;
function useMedia(query: string) {
  return useSyncExternalStore(
    useMemo(
      () => (notify: () => void) => {
        const media = window.matchMedia?.(query);
        media?.addEventListener("change", notify);
        return () => media?.removeEventListener("change", notify);
      },
      [query],
    ),
    () => window.matchMedia?.(query).matches ?? false,
    () => false,
  );
}
const Context = createContext({
  preferences: defaultPreferences,
  reducedMotion: false,
  theme: "light" as "light" | "dark",
  ...createFormatter("en"),
  update: (_value: Partial<Preferences>) => {
    void _value;
  },
  reset: () => {},
  retry: () => {},
  saveError: false,
});
export const useUiPreferences = () => useContext(Context);
export function UiPreferencesProvider({
  children,
  adapter,
}: {
  children: ReactNode;
  adapter?: PreferenceAdapter;
}) {
  const [preferences, setPreferences] = useState<Preferences>(() => {
    try {
      return sanitizePreferences(adapter?.load());
    } catch {
      return { ...defaultPreferences };
    }
  });
  const [saveError, setSaveError] = useState(false);
  const systemReduced = useMedia("(prefers-reduced-motion: reduce)");
  const systemDark = useMedia("(prefers-color-scheme: dark)");
  const reducedMotion = resolveReducedMotion(
    systemReduced,
    preferences.reduceMotion,
  );
  const theme =
    preferences.appearance === "system"
      ? systemDark
        ? "dark"
        : "light"
      : preferences.appearance;
  useEffect(() => {
    const root = document.documentElement;
    root.lang = preferences.locale === "en-XA" ? "en" : preferences.locale;
    root.dir = preferences.locale === "ar" ? "rtl" : "ltr";
    root.dataset.theme = theme;
    root.dataset.motion = reducedMotion ? "reduce" : "full";
  }, [preferences.locale, theme, reducedMotion]);
  function save(value: Preferences) {
    try {
      adapter?.save(value);
      setSaveError(false);
    } catch {
      setSaveError(true);
    }
  }
  function update(patch: Partial<Preferences>) {
    const next = sanitizePreferences({ ...preferences, ...patch });
    setPreferences(next);
    save(next);
    if (next.appearance !== preferences.appearance)
      trackAppearance(next.appearance);
    if (next.locale !== preferences.locale) trackPreference("locale");
    if (next.reduceMotion !== preferences.reduceMotion)
      trackPreference("motion");
  }
  function reset() {
    try {
      adapter?.clear();
      setSaveError(false);
    } catch {
      setSaveError(true);
    }
    if (preferences.appearance !== "system") trackAppearance("system");
    trackPreference("reset");
    setPreferences({ ...defaultPreferences });
  }
  return (
    <Context.Provider
      value={{
        preferences,
        reducedMotion,
        theme,
        ...createFormatter(preferences.locale),
        update,
        reset,
        retry: () => save(preferences),
        saveError,
      }}
    >
      {children}
    </Context.Provider>
  );
}
