import type { HTMLAttributes } from "react";
import "./action-footer.css";

/**
 * The one action row of every dialog, sheet and guided chat: at the bottom,
 * right-aligned (secondary/cancel first, primary last = right-most), full width
 * on phones. Keep it the last child of its dialog.
 */
export function ActionFooter({
  className = "",
  ...props
}: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      {...props}
      className={`action-footer ${className}`.trim()}
      data-action-footer=""
    />
  );
}
