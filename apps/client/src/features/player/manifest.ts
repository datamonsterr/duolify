import { session } from "../accounts/session";
import { bridgeScript } from "../lesson-player/bridge";
import { openLesson, preparedDocument } from "./cache";
export type Checkpoint = { id: string; at_ms: number };
export type ManifestSlide = {
  html_sha256: string;
  asset_sha256: Record<string, string>;
  slide_id: string;
  duration_ms: number;
  html_url: string;
  assets: string[];
  checkpoints: Checkpoint[];
  narration_status: "disabled" | "pending" | "failed";
  narration_text: string;
  explanation?: string;
};
export type Manifest = {
  lesson_id: string;
  course_id: string;
  title: string;
  version: number;
  asset_origin: string;
  slides: ManifestSlide[];
};
const id = /^[A-Za-z0-9_-]{1,100}$/;
const record = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === "object" && !Array.isArray(value);
function keys(value: Record<string, unknown>, names: string[]) {
  return (
    Object.keys(value).length === names.length &&
    names.every((name) => Object.prototype.hasOwnProperty.call(value, name))
  );
}
export function validAssetOrigin(origin: unknown): origin is string {
  if (typeof origin !== "string") return false;
  try {
    const url = new URL(origin);
    return (
      url.origin === origin &&
      !url.username &&
      !url.password &&
      (url.protocol === "https:" ||
        (url.protocol === "http:" &&
          ["localhost", "127.0.0.1"].includes(url.hostname))) &&
      !/[\s*'";<>\\]/.test(origin)
    );
  } catch {
    return false;
  }
}
export function isManifest(value: unknown): value is Manifest {
  if (
    !record(value) ||
    !keys(value, [
      "lesson_id",
      "course_id",
      "title",
      "version",
      "asset_origin",
      "slides",
    ]) ||
    typeof value.lesson_id !== "string" ||
    !id.test(value.lesson_id) ||
    typeof value.course_id !== "string" ||
    !id.test(value.course_id) ||
    typeof value.title !== "string" ||
    value.title.length > 2000 ||
    !Number.isSafeInteger(value.version) ||
    Number(value.version) < 1 ||
    !validAssetOrigin(value.asset_origin) ||
    !Array.isArray(value.slides) ||
    value.slides.length < 1 ||
    value.slides.length > 100
  )
    return false;
  const seen = new Set<string>();
  return value.slides.every((slide) => {
    if (
      !record(slide) ||
      !(
        keys(slide, [
          "slide_id",
          "html_sha256",
          "asset_sha256",
          "duration_ms",
          "html_url",
          "assets",
          "checkpoints",
          "narration_status",
          "narration_text",
          "explanation",
        ]) ||
        keys(slide, [
          "slide_id",
          "duration_ms",
          "html_url",
          "assets",
          "checkpoints",
          "narration_status",
          "narration_text",
          "explanation",
        ]) ||
        keys(slide, [
          "slide_id",
          "html_sha256",
          "asset_sha256",
          "duration_ms",
          "html_url",
          "assets",
          "checkpoints",
          "narration_status",
          "narration_text",
        ]) ||
        keys(slide, [
          "slide_id",
          "duration_ms",
          "html_url",
          "assets",
          "checkpoints",
          "narration_status",
          "narration_text",
        ])
      ) ||
      typeof slide.slide_id !== "string" ||
      (slide.html_sha256 !== undefined &&
        (typeof slide.html_sha256 !== "string" ||
          !/^[a-f0-9]{64}$/.test(slide.html_sha256))) ||
      (slide.asset_sha256 !== undefined && !record(slide.asset_sha256)) ||
      !id.test(slide.slide_id) ||
      seen.has(slide.slide_id) ||
      !Number.isSafeInteger(slide.duration_ms) ||
      Number(slide.duration_ms) < 1 ||
      Number(slide.duration_ms) > 120000 ||
      slide.html_url !==
        `/v1/lessons/${value.lesson_id}/slides/${slide.slide_id}` ||
      !["disabled", "pending", "failed"].includes(
        String(slide.narration_status),
      ) ||
      typeof slide.narration_text !== "string" ||
      slide.narration_text.length > 20000 ||
      (slide.explanation !== undefined &&
        (typeof slide.explanation !== "string" ||
          slide.explanation.length > 20000)) ||
      !Array.isArray(slide.assets) ||
      slide.assets.length > 100 ||
      (slide.asset_sha256 !== undefined &&
        (Object.keys(slide.asset_sha256).length !== slide.assets.length ||
          !slide.assets.every(
            (a) =>
              typeof a === "string" &&
              /^[a-f0-9]{64}$/.test(
                String((slide.asset_sha256 as Record<string, unknown>)[a]),
              ),
          ))) ||
      !slide.assets.every((a) => {
        try {
          const u = new URL(a);
          return (
            typeof a === "string" &&
            a.length <= 2000 &&
            u.origin === value.asset_origin &&
            !u.username &&
            !u.password &&
            !u.hash
          );
        } catch {
          return false;
        }
      }) ||
      !Array.isArray(slide.checkpoints) ||
      slide.checkpoints.length > 100
    )
      return false;
    seen.add(slide.slide_id);
    const checkpoints = new Set<string>();
    let last = -1;
    return slide.checkpoints.every((c) => {
      if (
        !record(c) ||
        !keys(c, ["id", "at_ms"]) ||
        typeof c.id !== "string" ||
        !id.test(c.id) ||
        checkpoints.has(c.id) ||
        !Number.isSafeInteger(c.at_ms) ||
        Number(c.at_ms) < 0 ||
        Number(c.at_ms) < last ||
        Number(c.at_ms) > Number(slide.duration_ms)
      )
        return false;
      checkpoints.add(c.id);
      last = Number(c.at_ms);
      return true;
    });
  });
}
export async function loadManifest(lessonId: string): Promise<Manifest> {
  return openLesson(lessonId);
}
export async function loadDocument(slide: ManifestSlide): Promise<string> {
  const prepared = preparedDocument(slide);
  if (prepared !== undefined) return prepared;
  const value = await session.request<unknown>(slide.html_url);
  if (
    !record(value) ||
    typeof value.html !== "string" ||
    value.html.length === 0 ||
    value.html.length > 1048576
  )
    throw new Error("document_unavailable");
  return value.html;
}
// The first parsed bytes install policy before any untrusted markup. Nested
// document tags are parsed inside this opaque-origin document, never the host.
export function sandboxDocument(
  html: string,
  origin: string,
  reduced: boolean,
  slide?: ManifestSlide,
  cached = false,
) {
  if (!validAssetOrigin(origin)) throw new Error("invalid_manifest");
  const resource = cached ? "data:" : origin;
  const csp = `default-src 'none'; script-src 'unsafe-inline' ${resource}; style-src 'unsafe-inline' ${resource}; img-src ${resource}; media-src ${resource}; font-src ${resource}; connect-src ${cached ? "'none'" : origin}; frame-src 'none'; object-src 'none'; worker-src 'none'; base-uri 'none'; form-action 'none';`;
  return `<!doctype html><html data-motion="${reduced ? "reduce" : "full"}"><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${csp}"><meta name="referrer" content="no-referrer"><style>:root[data-motion="reduce"] *, :root[data-motion="reduce"] *::before, :root[data-motion="reduce"] *::after {animation:none!important;transition:none!important;scroll-behavior:auto!important} body {overflow-wrap:anywhere}</style>${slide ? bridgeScript({ duration: slide.duration_ms, checkpoints: slide.checkpoints }) : ""}</head><body>${html}</body></html>`;
}
