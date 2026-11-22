// Stage geometry for the immersive presentation. The Slide keeps one 16:9
// coordinate space (1280×720); the host only scales it uniformly, letterboxes it
// and, where the platform cannot lock orientation, rotates it 90°.
export const STAGE_W = 1280;
export const STAGE_H = 720;

export type RotationMode = "landscape" | "none";
export type StageLayout = {
  rotated: boolean;
  scale: number;
  /** Displayed size of the stage in CSS px, along the stage's own axes. */
  width: number;
  height: number;
};

/** `landscape` = the player wants landscape and may rotate the stage to get it. */
export function layoutStage(
  viewportW: number,
  viewportH: number,
  mode: RotationMode,
): StageLayout {
  const rotated = mode === "landscape" && viewportH > viewportW;
  const w = rotated ? viewportH : viewportW;
  const h = rotated ? viewportW : viewportH;
  const scale = Math.max(0.01, Math.min(w / STAGE_W, h / STAGE_H));
  return { rotated, scale, width: STAGE_W * scale, height: STAGE_H * scale };
}

export function stageTransform(l: StageLayout): string {
  return `rotate(${l.rotated ? 90 : 0}deg) scale(${l.scale})`;
}

/** Screen point → normalized stage coordinates (0..1), undoing rotation and scale. */
export function clientToStage(
  x: number,
  y: number,
  viewportW: number,
  viewportH: number,
  l: StageLayout,
  clampToStage = false,
): { x: number; y: number } {
  const dx = x - viewportW / 2;
  const dy = y - viewportH / 2;
  // CSS rotate(90deg): stage +x points screen-down, stage +y points screen-left.
  const sx = l.rotated ? dy : dx;
  const sy = l.rotated ? -dx : dy;
  let nx = (sx / l.scale + STAGE_W / 2) / STAGE_W;
  let ny = (sy / l.scale + STAGE_H / 2) / STAGE_H;
  if (clampToStage) {
    nx = Math.min(1, Math.max(0, nx));
    ny = Math.min(1, Math.max(0, ny));
  }
  return { x: nx, y: ny };
}
