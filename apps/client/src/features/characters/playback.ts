import { compileRigSvg, getBundledFallbackSvg } from "./rig";
import { validateCharacterSpec } from "./schema";
import type {
  Expression,
  LessonCastSnapshot,
  SlideManifest,
  SlideSegment,
} from "./types";

const EXPRESSIONS: Expression[] = [
  "idle",
  "happy",
  "sad",
  "thinking",
  "celebrate",
];

const esc = (value: string) =>
  value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");

// JSON embedded in <script> must not be able to close it or break the parser.
const scriptJson = (value: unknown) =>
  JSON.stringify(value)
    .replace(/</g, "\\u003c")
    .replace(/>/g, "\\u003e")
    .replace(/&/g, "\\u0026")
    .replace(/\u2028/g, "\\u2028")
    .replace(/\u2029/g, "\\u2029");

/** Segment shown at a host-clock time; falls back to the last started one. */
export function activeCharacterSegment(
  segments: SlideSegment[] | undefined,
  ms: number,
): SlideSegment | undefined {
  if (!segments?.length) return undefined;
  const inside = segments.find(
    (s) => ms >= s.start_ms && ms < s.start_ms + s.duration_ms,
  );
  if (inside) return inside;
  const started = segments.filter((s) => s.start_ms <= ms);
  return started.length ? started[started.length - 1] : segments[0];
}

type Fallback = "unsupported_rig" | "invalid_spec" | "missing_speaker" | null;

function speakerFallback(cast: LessonCastSnapshot, key: string): Fallback {
  const speaker = cast.speakers[key];
  if (!speaker) return "missing_speaker";
  if ((speaker.spec as { rig_version?: string }).rig_version !== "original-v1")
    return "unsupported_rig";
  return validateCharacterSpec(speaker.spec).valid ? null : "invalid_spec";
}

/**
 * Host-clocked character stage for a generated Slide. It has no timer: the
 * player's `seek`/`tick` messages are the only clock, so pause, seek, replay
 * and rate changes cannot drift. Unusable speakers use bundled defaults and
 * never mutate the stored cast; captions always stay readable.
 */
export function generatedCharacterDocument(
  slide: SlideManifest,
  cast: LessonCastSnapshot,
  reducedMotion: boolean,
  offline: boolean,
): string {
  const segments = slide.segments ?? [];
  const keys = [
    ...new Set([
      ...Object.keys(cast.speakers),
      ...segments.map((s) => s.speaker_key),
    ]),
  ].slice(0, 4);
  const speakers: Record<string, unknown> = {};
  for (const key of keys) {
    const reason = speakerFallback(cast, key);
    const pinned = cast.speakers[key];
    const svgs: Record<string, string> = {};
    for (const expression of EXPRESSIONS) {
      svgs[expression] =
        reason || !pinned
          ? getBundledFallbackSvg(expression)
          : compileRigSvg(pinned.spec, expression);
    }
    speakers[key] = {
      svgs,
      fallback: reason,
      character_version: pinned?.character_version ?? 1,
      rig_version: reason
        ? "original-v1"
        : (pinned?.spec.rig_version ?? "original-v1"),
    };
  }
  const first = segments[0];
  const data = {
    segments,
    speakers,
    offline,
    slide_id: /^[A-Za-z0-9_-]{1,100}$/.test(slide.slide_id)
      ? slide.slide_id
      : "slide",
  };
  const avatars = keys
    .map(
      (key) =>
        `<div class="cast-avatar" data-speaker-key="${esc(key)}" data-expression="idle">${(speakers[key] as { svgs: Record<string, string> }).svgs.idle}</div>`,
    )
    .join("");
  const caption = first ? esc(first.text) : "";
  const label = first ? esc(first.speaker_label) : "";
  const reasons = keys
    .map((k) => (speakers[k] as { fallback: Fallback }).fallback)
    .filter(Boolean);
  return `<style>
.cast-stage{display:flex;flex-direction:column;align-items:center;gap:12px;padding:12px;font-family:system-ui,sans-serif}
.cast-row{display:flex;gap:16px;justify-content:center}
.cast-avatar{opacity:.55;transition:opacity .2s}
.cast-avatar.active{opacity:1}
.cast-avatar svg{width:96px;height:96px}
.cast-caption{max-width:36rem;text-align:center}
:root[data-motion="reduce"] .cast-avatar{transition:none}
</style>
<section class="cast-stage" data-motion="${reducedMotion ? "reduce" : "full"}" data-cast-fallback="${esc(reasons.join(","))}">
<div class="cast-row">${avatars}</div>
<div class="cast-caption" role="group" aria-label="Dialogue"><strong id="speaker-label">${label}</strong><p id="dialogue-text" aria-live="off">${caption}</p></div>
</section>
<script>(function(){
var D=${scriptJson(data)},seen={},last=-1;
function show(i){
  var s=D.segments[i];if(!s)return;
  var k=s.speaker_key,sp=D.speakers[k];
  document.getElementById("speaker-label").textContent=s.speaker_label;
  document.getElementById("dialogue-text").textContent=s.text;
  var els=document.querySelectorAll("[data-speaker-key]");
  for(var j=0;j<els.length;j++)els[j].classList.toggle("active",els[j].getAttribute("data-speaker-key")===k);
  var el=document.querySelector('[data-speaker-key="'+k+'"]');
  if(el&&sp){el.setAttribute("data-expression",s.expression);el.innerHTML=sp.svgs[s.expression]||sp.svgs.idle;}
  if(seen[k])return;seen[k]=1;
  try{
    parent.postMessage(sp&&sp.fallback?{type:"character_render_fallback",reason_code:sp.fallback,offline:D.offline}:{type:"character_rendered",speaker_key:k,character_version:sp?sp.character_version:1,rig_version:sp?sp.rig_version:"original-v1",expression:s.expression,offline:D.offline,slide_id:D.slide_id},"*");
  }catch(e){}
}
function at(t){
  var idx=-1,j;
  for(j=0;j<D.segments.length;j++){var s=D.segments[j];if(t>=s.start_ms&&t<s.start_ms+s.duration_ms){idx=j;break;}}
  if(idx<0)for(j=0;j<D.segments.length;j++)if(D.segments[j].start_ms<=t)idx=j;
  return idx<0?0:idx;
}
window.addEventListener("message",function(event){
  if(event.source !== parent)return;
  var d=event.data;
  if(!d||typeof d!=="object"||Array.isArray(d))return;
  if(d.type==="set_motion"&&typeof d.reduced==="boolean"){document.documentElement.dataset.motion=d.reduced?"reduce":"full";return;}
  if((d.type==="seek"||d.type==="tick")&&typeof d.time==="number"&&isFinite(d.time)){
    var i=at(d.time);if(d.type==="seek"||i!==last){last=i;show(i);}
  }
});
if(D.segments.length){last=0;show(0);}
})();</script>`;
}
