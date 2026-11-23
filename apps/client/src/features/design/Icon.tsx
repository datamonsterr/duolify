// Icon set for lazily loaded screens (the shell keeps its own first-load icons in AppShell).
// Same function, same icon, same place: icon-only controls use the universal set in `humanFactors.ts`.
const paths = {
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

  menu: "M4 6h16 M4 12h16 M4 18h16",
  look: "M12 3a9 9 0 1 0 0 18c1.5 0 2-1 1.5-2-.6-1.2 0-2.5 1.5-2.500H17a4 4 0 0 0 4-4 9 9 0 0 0-9-9z",
  audio: "M4 9v6h4l5 4V5L8 9z M16 9a4 4 0 0 1 0 6 M18.5 6.5a8 8 0 0 1 0 11",
  goal: "M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z M12 16a4 4 0 1 0 0-8 4 4 0 0 0 0 8z M12 12h.01",
  lock: "M6 11h12v10H6z M8 11V7a4 4 0 0 1 8 0v4",
  chevron: "m9 6 6 6-6 6",
  close: "M6 6l12 12 M18 6 6 18",
  back: "m15 6-6 6 6 6",
  search: "M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14z M20 20l-4-4",
  add: "M12 5v14 M5 12h14",
  share: "M12 3v13 M7 8l5-5 5 5 M5 14v7h14v-7",
  play: "M7 4v16l13-8z",
  pause: "M7 4v16 M17 4v16",
  previous: "M6 5v14 M20 5v14L8 12z",
  next: "M18 5v14 M4 5v14l12-7z",
  send: "M3 11 21 3l-8 18-2-8z",
  microphone:
    "M12 3a3 3 0 0 0-3 3v6a3 3 0 0 0 6 0V6a3 3 0 0 0-3-3z M5 11a7 7 0 0 0 14 0 M12 18v3",
  volume: "M4 9v6h4l5 4V5L8 9z M16 9a4 4 0 0 1 0 6",
  mute: "M4 9v6h4l5 4V5L8 9z M16 9l5 6 M21 9l-5 6",
  fullscreen: "M4 9V4h5 M20 9V4h-5 M4 15v5h5 M20 15v5h-5",
  captions: "M3 6h18v12H3z M8 10h2 M14 10h2 M7 14h4 M13 14h4",
};
export type IconName = keyof typeof paths;

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
