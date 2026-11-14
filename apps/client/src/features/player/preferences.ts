const PREFERENCES_STORAGE_KEY = "duolify.player-preferences";

export type StoredPlayerPreferences = {
  rate: number;
  muted: boolean;
  captions: boolean;
};

const DEFAULT_PREFERENCES: StoredPlayerPreferences = {
  rate: 1,
  muted: false,
  captions: true,
};

export function loadPlayerPreferences(): StoredPlayerPreferences {
  if (typeof localStorage === "undefined") return { ...DEFAULT_PREFERENCES };
  try {
    const raw = localStorage.getItem(PREFERENCES_STORAGE_KEY);
    if (!raw) return { ...DEFAULT_PREFERENCES };
    const parsed = JSON.parse(raw);
    const rate = [0.75, 1, 1.25, 1.5, 2].includes(parsed.rate)
      ? Number(parsed.rate)
      : 1;
    const muted = Boolean(parsed.muted);
    const captions = parsed.captions !== false;
    return { rate, muted, captions };
  } catch {
    return { ...DEFAULT_PREFERENCES };
  }
}

export function savePlayerPreferences(
  pref: Partial<StoredPlayerPreferences>,
): void {
  if (typeof localStorage === "undefined") return;
  try {
    const current = loadPlayerPreferences();
    const updated = { ...current, ...pref };
    localStorage.setItem(PREFERENCES_STORAGE_KEY, JSON.stringify(updated));
  } catch {
    // Ignore localStorage write quota/access errors
  }
}
