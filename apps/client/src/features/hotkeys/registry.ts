import type data from "./shortcuts.json";
import manifest from "./shortcuts.json?raw";
import type { ShortcutMessage } from "./messages";

export type ShortcutId = keyof typeof data;
export type ShortcutScope =
  "app" | "player" | "lesson_test" | "composer" | "control";
export type HelpGroup = "playback" | "navigate" | "notes" | "app";
export type ShortcutPlatform = "mac" | "other";
type Menu = {
  group: "app" | "navigate" | "view" | "help";
  label: string;
  accelerator?: string;
};
type Definition = {
  scope: ShortcutScope;
  label: ShortcutMessage;
  tooltip?: ShortcutMessage;
  bindings: readonly string[];
  group?: HelpGroup;
  repeat?: boolean;
  desktop?: boolean;
  menu?: Menu;
};

export type Shortcut = Definition & { id: ShortcutId };
export const shortcuts: readonly Shortcut[] = Object.entries(
  JSON.parse(manifest) as Record<ShortcutId, Definition>,
).map(([id, item]) => ({ id: id as ShortcutId, ...item }));
export const shortcutById = Object.fromEntries(
  shortcuts.map((item) => [item.id, item]),
) as Record<ShortcutId, Shortcut>;
export const nativeMenu = shortcuts.filter(
  (item): item is Shortcut & { menu: Menu } => item.menu !== undefined,
);

export function shortcutPlatform(): ShortcutPlatform {
  return /Mac|iPhone|iPad/.test(navigator.platform) ? "mac" : "other";
}

export function bindingParts(binding: string) {
  const [first, second] = binding.split("+");
  return second ? { mod: first, key: second } : { key: first };
}

const namedKeys: Readonly<Record<string, string>> = {
  Escape: "Esc",
  ArrowLeft: "←",
  ArrowRight: "→",
  ArrowUp: "↑",
  ArrowDown: "↓",
  PageUp: "PgUp",
  PageDown: "PgDn",
};

export function shortcutKeys(id: ShortcutId, platform: ShortcutPlatform) {
  return shortcutById[id].bindings.map((binding) => {
    const { mod, key } = bindingParts(binding);
    const label = namedKeys[key] ?? key.replace(/^Key/, "");
    if (mod === "Primary")
      return platform === "mac" ? `⌘${label}` : `Ctrl+${label}`;
    if (mod === "Shift")
      return platform === "mac" ? `⇧${label}` : `Shift+${label}`;
    return label;
  });
}

export function shortcutAria(id: ShortcutId, platform = shortcutPlatform()) {
  return (
    shortcutById[id].bindings
      .flatMap((binding) => {
        const { mod, key } = bindingParts(binding);
        const label = key === "," ? "Comma" : key.replace(/^Key/, "");
        if (key === "0–9" || key === "1–9")
          return Array.from({ length: key === "0–9" ? 10 : 9 }, (_, index) =>
            String(index + Number(key[0])),
          );
        if (mod === "Primary")
          return `${platform === "mac" ? "Meta" : "Control"}+${label}`;
        return mod ? `${mod}+${label}` : label;
      })
      .join(" ") || undefined
  );
}

export function getHelpSections(scope: ShortcutScope, desktop: boolean) {
  return (["playback", "navigate", "notes", "app"] as const).flatMap(
    (group) => {
      const rows = new Map<string, Shortcut[]>();
      for (const item of shortcuts) {
        if (item.group !== group || (item.desktop && !desktop)) continue;
        if (
          item.scope !== scope &&
          item.scope !== "app" &&
          !(scope === "player" && item.scope === "composer")
        )
          continue;
        const row = rows.get(item.label);
        if (row) row.push(item);
        else rows.set(item.label, [item]);
      }
      return rows.size ? [{ group, rows: [...rows.values()] }] : [];
    },
  );
}
