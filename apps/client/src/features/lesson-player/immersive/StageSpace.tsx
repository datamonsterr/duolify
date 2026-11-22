import type { ReactNode } from "react";

/**
 * A box in Slide coordinates (1280 × 720) laid exactly over the immersive stage:
 * same centring, uniform scale and rotation as the Slide frame (`--imm-scale`,
 * `--imm-rot`, set by the immersive host). Children are positioned in percent of
 * the stage, so a normalized region (0..1) maps to `left: x * 100%` and so on.
 * Use `calc(44px / var(--imm-scale, 1))` for anything that must stay finger-sized.
 */
export function StageSpace({
  children,
  testId,
}: {
  children: ReactNode;
  testId?: string;
}) {
  return (
    <div className="imm-stage-space" data-testid={testId}>
      {children}
    </div>
  );
}
