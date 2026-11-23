/** WCAG contrast helpers for the translucent overlay surface (spec: 4.5:1 over any Slide). */
export const GLASS_ALPHA = 0.78;

type Rgb = [number, number, number];
export const parseHex = (hex: string): Rgb => {
  const n = parseInt(hex.replace("#", ""), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
};
export const luminance = ([r, g, b]: Rgb) =>
  [r, g, b]
    .map((v) => v / 255)
    .map((v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4))
    .reduce((sum, v, i) => sum + v * [0.2126, 0.7152, 0.0722][i], 0);
export const contrast = (a: Rgb, b: Rgb) => {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
};
/** The colour the Learner sees: `surface` at `alpha` laid over the Slide pixel `behind`. */
export const composite = (surface: Rgb, alpha: number, behind: Rgb): Rgb =>
  surface.map((c, i) => Math.round(c * alpha + behind[i] * (1 - alpha))) as Rgb;
