// Testable human-factors rules (openspec simplify-ux-gestures-hotkeys, "Human-factors rules").
import { iconOnlyAllowlist } from "./Icon";

/** Hick's law: count controls marked with `data-action-level` in a view. */
export function countActionLevels(root: ParentNode) {
  const count = (level: string) =>
    [...root.querySelectorAll(`[data-action-level="${level}"]`)].filter(
      (el) => !el.closest("[hidden]"),
    ).length;
  return { primary: count("primary"), secondary: count("secondary") };
}
export function actionLevelViolations(
  root: ParentNode,
  max = { primary: 1, secondary: 2 },
) {
  const c = countActionLevels(root);
  const out: string[] = [];
  if (c.primary > max.primary)
    out.push(`${c.primary} primary actions (max ${max.primary})`);
  if (c.secondary > max.secondary)
    out.push(`${c.secondary} secondary actions (max ${max.secondary})`);
  return out;
}

/** Recognition over recall: icon-only controls must use the universal set and have a name. */
export function iconOnlyViolations(root: ParentNode) {
  const out: string[] = [];
  const controls = root.querySelectorAll(
    "button, a[href], [role=button], [role=tab]",
  );
  for (const el of controls) {
    if (el.closest("[hidden]")) continue;
    const icons = el.querySelectorAll("[data-icon]");
    const text = [...el.childNodes]
      .map((n) =>
        n instanceof Element && n.hasAttribute("data-icon")
          ? ""
          : (n.textContent ?? ""),
      )
      .join("")
      .replace(/\s+/g, "");
    if (!icons.length || text) continue;
    const name = icons[0].getAttribute("data-icon") ?? "";
    const label = el.getAttribute("aria-label") || el.getAttribute("title");
    if (!(iconOnlyAllowlist as readonly string[]).includes(name))
      out.push(`icon-only "${name}" is not in the universal set`);
    if (!label) out.push(`icon-only "${name}" has no accessible name`);
  }
  return out;
}

/** Fitts's law geometry for one phone viewport; callers pass measured boxes. */
export type Box = { x: number; y: number; width: number; height: number };
export function reachViolations(
  primary: Box,
  viewport: { width: number; height: number },
  safeBottom = 0,
) {
  const out: string[] = [];
  if (primary.y < viewport.height * 0.6)
    out.push("primary action is above the lower 40%");
  if (primary.width < 44 || primary.height < 44)
    out.push("primary action is smaller than 44 by 44");
  if (primary.y + primary.height > viewport.height - safeBottom)
    out.push("primary action overlaps the bottom inset");
  return out;
}
/** Gaps of at least 8 px between neighbouring targets in a row or column. */
export function gapViolations(boxes: Box[], min = 8) {
  const out: string[] = [];
  for (let i = 0; i < boxes.length; i++)
    for (let j = i + 1; j < boxes.length; j++) {
      const a = boxes[i];
      const b = boxes[j];
      const dx = Math.max(a.x - (b.x + b.width), b.x - (a.x + a.width));
      const dy = Math.max(a.y - (b.y + b.height), b.y - (a.y + a.height));
      if (Math.max(dx, dy) < min)
        out.push(`targets ${i} and ${j} are closer than ${min}px`);
    }
  return out;
}
