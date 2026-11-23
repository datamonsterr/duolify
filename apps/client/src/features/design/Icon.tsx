// One icon set for the whole app (human-factors rule: same function, same icon, same place).
// The shell and feedback icons ship with the first load; the rest live in `iconsExtra.ts`, which the
// screens that need them import and register.
const paths: Record<string, string> = {
  home: "M3 11 12 4l9 7v9h-6v-6H9v6H3z",
  courses: "m2 8 10-5 10 5-10 5z M2 13l10 5 10-5 M2 18l10 5 10-5",
  friends:
    "M8 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8 M2 21c0-8 12-8 12 0 M17 4a4 4 0 0 1 0 8 M17 15c4 0 5 3 5 6",
  flag: "M5 21V3h13l-3 5 3 5H5",
  note: "M6 3h9l4 4v14H6z M14 3v5h5 M9 13h7 M9 17h5",
  bookmark: "M6 3h12v18l-6-4-6 4z",
  gem: "M7 4h10l4 5-9 11L3 9z M3 9h18 M10 4 8 9l4 11 4-11-2-5",
  profile: "M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8 M4 21c0-9 16-9 16 0",
  settings: "M4 7h16 M4 17h16 M8 4v6 M16 14v6",
  more: "M5 12h.01 M12 12h.01 M19 12h.01",
  info: "M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z M12 11v6 M12 8h.01",
  warning: "M12 3 2 20h20z M12 10v5 M12 18h.01",
  error: "M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z M9 9l6 6 M15 9l-6 6",
  check: "m5 12 5 5 9-10",
};
export type IconName =
  | "home"
  | "courses"
  | "friends"
  | "flag"
  | "note"
  | "bookmark"
  | "gem"
  | "profile"
  | "settings"
  | "more"
  | "info"
  | "warning"
  | "error"
  | "check"
  | ExtraIconName;
export type ExtraIconName =
  | "menu"
  | "look"
  | "audio"
  | "goal"
  | "lock"
  | "chevron"
  | "close"
  | "back"
  | "search"
  | "add"
  | "share"
  | "play"
  | "pause"
  | "previous"
  | "next"
  | "send"
  | "microphone"
  | "volume"
  | "mute"
  | "fullscreen"
  | "captions";

export function registerIcons(more: Record<string, string>) {
  Object.assign(paths, more);
}

export function Icon({
  name,
  className = "",
  accent,
}: {
  name: IconName;
  className?: string;
  accent?: string;
}) {
  return (
    <svg
      className={`nav-icon ${accent ? `nav-icon-${accent}` : ""} ${className}`}
      data-icon={name}
      viewBox="0 0 24 24"
      aria-hidden="true"
    >
      <path d={paths[name]} />
    </svg>
  );
}
