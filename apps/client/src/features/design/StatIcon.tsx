const paths = {
  flame: "M12 2c1 4 6 6 6 12a6 6 0 0 1-12 0c0-3 2-4 2-7 2 1 3 2 4-5z",
  gem: "M7 4h10l4 5-9 11L3 9z M3 9h18 M10 4 8 9l4 11 4-11-2-5",
  bolt: "M13 2 4 14h7l-1 8 9-12h-7z",
  refresh: "M20 12a8 8 0 1 1-2.3-5.7 M20 4v5h-5",
} as const;

/** Decorative stroke icon; the control that holds it carries the accessible name. */
export function StatIcon({ name }: { name: keyof typeof paths }) {
  return (
    <svg
      className="stat-icon"
      viewBox="0 0 24 24"
      width="18"
      height="18"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={paths[name]} />
    </svg>
  );
}
