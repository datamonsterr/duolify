export type ProgressState = "pending" | "skipped" | "completed";
export function mergeState(a: ProgressState, b: ProgressState): ProgressState {
  const rank = { pending: 0, skipped: 1, completed: 2 };
  return rank[a] >= rank[b] ? a : b;
}
export function advance(
  index: number,
  action: "next" | "back" | "skip",
  total: number,
): number | "skipped" | "completed" {
  if (action === "skip") return "skipped";
  if (action === "back") return Math.max(0, index - 1);
  return index + 1 >= total ? "completed" : index + 1;
}
export type Rect = { x: number; y: number; width: number; height: number };
export type Placement = {
  x: number;
  y: number;
  side: "top" | "bottom" | "left" | "right";
  arrow: number;
};
const clamp = (n: number, min: number, max: number) =>
  Math.max(min, Math.min(n, Math.max(min, max)));
export function placeTooltip(
  target: Rect,
  card: { width: number; height: number },
  view: Rect,
): Placement {
  const gap = 24,
    margin = 12;
  const cx = target.x + target.width / 2,
    cy = target.y + target.height / 2;
  const minX = view.x + margin,
    minY = view.y + margin;
  const maxX = view.x + view.width - margin - card.width;
  const maxY = view.y + view.height - margin - card.height;
  let side: Placement["side"] = "top";
  let x = cx - card.width / 2,
    y = target.y + target.height + gap;
  if (y > maxY) {
    if (target.y - gap - card.height >= minY) {
      side = "bottom";
      y = target.y - gap - card.height;
    } else if (
      target.x + target.width + gap + card.width <=
      view.x + view.width - margin
    ) {
      side = "left";
      x = target.x + target.width + gap;
      y = cy - card.height / 2;
    } else if (target.x - gap - card.width >= minX) {
      side = "right";
      x = target.x - gap - card.width;
      y = cy - card.height / 2;
    } else {
      // On short viewports the card occupies the roomier side and scrolls.
      side = target.y > view.y + view.height / 2 ? "bottom" : "top";
      y =
        side === "bottom"
          ? target.y - gap - card.height
          : target.y + target.height + gap;
    }
  }
  x = clamp(x, minX, maxX);
  y = clamp(y, minY, maxY);
  return {
    x,
    y,
    side,
    arrow:
      side === "top" || side === "bottom"
        ? clamp(cx - x, 24, card.width - 24)
        : clamp(cy - y, 24, card.height - 24),
  };
}
export const stepIds = [
  "path",
  "next_lesson",
  "credits",
  "progress",
  "notes",
  "courses",
  "settings",
] as const;
export type StepId = (typeof stepIds)[number];
const selectors: Record<StepId, string> = {
  path: '[data-tour="path"]',
  next_lesson: ".path a.node.current, .path button.node.ready:not(:disabled)",
  credits: '[data-tour="credits"]',
  progress: '[data-tour="progress"]',
  notes: '[data-tour="notes"]',
  courses: '[data-tour="courses"]',
  settings: '[data-tour="settings"], [data-tour="more"]',
};
export function targetFor(id: StepId): HTMLElement | undefined {
  return [...document.querySelectorAll<HTMLElement>(selectors[id])].find(
    (node) =>
      node.isConnected &&
      !node.hidden &&
      !node.closest("[hidden],[inert]") &&
      node.getBoundingClientRect().width > 0 &&
      node.getBoundingClientRect().height > 0 &&
      getComputedStyle(node).visibility !== "hidden",
  );
}
export const availableSteps = () => stepIds.filter((id) => targetFor(id));
