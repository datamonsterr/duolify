import {
  defaultGuideFixture,
  defaultSupporterFixture,
  defaultCompanionFixture,
} from "./fixtures";
import { POSE_FOR_EXPRESSION } from "./motion-slide";
import { compileRigSvg } from "./rig";
import { altText } from "./rig-v2-alt";
import { compileV2, POSES } from "./rig-v2";
import { validateCharacterSpec } from "./schema";
import { validateSpecV2 } from "./schema-v2";
import { upgradeV1 } from "./upgrade";
import { EXPRESSIONS_V2, type ExpressionV2, type Pose } from "./types-v2";
import type {
  CharacterSpec,
  Expression,
  LessonCastSnapshot,
  SlideManifest,
  SlideSegment,
} from "./types";

type Appearance = { pose: Pose; expression: ExpressionV2 };

function appearance(segment: SlideSegment): Appearance {
  const expression = EXPRESSIONS_V2.includes(segment.expression)
    ? segment.expression
    : "idle";
  const pose =
    segment.pose && POSES.includes(segment.pose)
      ? segment.pose
      : POSE_FOR_EXPRESSION[expression];
  return { pose, expression };
}

const stateKey = ({ pose, expression }: Appearance) => `${pose}:${expression}`;

const defaults: Record<string, CharacterSpec> = {
  guide: defaultGuideFixture,
  supporter: defaultSupporterFixture,
  supporter_2: defaultCompanionFixture,
};

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

export type Fallback =
  "unsupported_rig" | "invalid_spec" | "missing_speaker" | "suppressed" | null;

export function speakerFallback(
  cast: LessonCastSnapshot,
  key: string,
): Fallback {
  const speaker = Object.prototype.hasOwnProperty.call(cast.speakers, key)
    ? cast.speakers[key]
    : undefined;
  if (!speaker) return "missing_speaker";
  if (speaker.suppressed) return "suppressed";
  if ((speaker.spec as { rig_version?: string }).rig_version !== "original-v1")
    return "unsupported_rig";
  if (!validateCharacterSpec(speaker.spec).valid) return "invalid_spec";
  return speaker.document && !validateSpecV2(speaker.document).valid
    ? "invalid_spec"
    : null;
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
  const segments = (slide.segments ?? []).map((segment) => ({
    ...segment,
    ...appearance(segment),
  }));
  const keys = [...new Set(segments.map((s) => s.speaker_key))].slice(0, 3);
  const speakers = Object.fromEntries(
    keys.map((key) => {
      const fallback = speakerFallback(cast, key);
      const pinned = cast.speakers[key];
      const original =
        !fallback && pinned
          ? pinned.spec
          : Object.prototype.hasOwnProperty.call(defaults, key)
            ? defaults[key]!
            : defaultGuideFixture;
      const document = fallback ? undefined : pinned?.document;
      const upgraded = document ?? upgradeV1(original);
      const states = segments.filter((s) => s.speaker_key === key);
      // Old documents without an authored pose keep their v1 art. Their clock
      // still derives a pose, and new segments can pose an upgraded v1 rig.
      const posed =
        Boolean(document) ||
        slide.segments?.some((s) => s.speaker_key === key && s.pose);
      const svgs: Record<string, string> = {};
      for (const state of [
        { pose: "idle", expression: "idle" } as const,
        ...states,
      ]) {
        const id = stateKey(state);
        svgs[id] ??= posed
          ? compileV2(upgraded, "body", state.pose, state.expression)
          : compileRigSvg(
              original,
              (state.expression === "surprised" ||
              state.expression === "confused"
                ? "idle"
                : state.expression) as Expression,
            );
      }
      return [
        key,
        {
          svgs,
          fallback,
          label: altText(upgraded),
          name: original.info.name,
          character_version: fallback ? 1 : (pinned?.character_version ?? 1),
          rig_version: posed ? "original-v2" : "original-v1",
        },
      ];
    }),
  );
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
    .map((key) => {
      const speaker = speakers[key]!;
      const state =
        first?.speaker_key === key
          ? first
          : ({ pose: "idle", expression: "idle" } as const);
      return `<div class="cast-avatar" data-speaker-key="${esc(key)}" data-pose="${state.pose}" data-expression="${state.expression}" role="img" aria-label="${esc(speaker.label)}">${speaker.svgs[stateKey(state)]}</div>`;
    })
    .join("");
  const caption = first ? esc(first.text) : "";
  const firstSpeaker = first ? speakers[first.speaker_key] : undefined;
  const label = first
    ? esc(firstSpeaker?.fallback ? firstSpeaker.name : first.speaker_label)
    : "";
  const reasons = keys.map((k) => speakers[k]!.fallback).filter(Boolean);
  return `<style>
.cast-stage{display:flex;flex-direction:column;align-items:center;gap:12px;padding:12px;font-family:system-ui,sans-serif}
.cast-row{display:flex;gap:16px;justify-content:center}
.cast-avatar{opacity:.55;transition:opacity .2s}
.cast-avatar.active{opacity:1}
.cast-avatar svg{width:152px;height:152px}
.cast-caption{max-width:36rem;text-align:center;font-size:22px}
:root[data-motion="reduce"] .cast-avatar{transition:none}
</style>
<section class="cast-stage" data-motion="${reducedMotion ? "reduce" : "full"}" data-cast-fallback="${esc(reasons.join(","))}">
<div class="cast-row">${avatars}</div>
<div class="cast-caption" role="group" aria-label="Dialogue"><strong id="speaker-label">${label}</strong><p id="dialogue-text" aria-live="off">${caption}</p></div>
</section>
<script>(function(){
var D=${scriptJson(data)},seen=new Set(),last=-1;
function show(i){
  var s=D.segments[i];if(!s)return;
  var k=s.speaker_key,sp=D.speakers[k];
  document.getElementById("speaker-label").textContent=sp&&sp.fallback?sp.name:s.speaker_label;
  document.getElementById("dialogue-text").textContent=s.text;
  var els=document.querySelectorAll(".cast-row [data-speaker-key]");
  for(var j=0;j<els.length;j++){
    var el=els[j], key=el.dataset.speakerKey, speaker=D.speakers[key];
    var state={pose:"idle",expression:"idle"};
    for(var n=i;n>=0;n--)if(D.segments[n].speaker_key===key){state=D.segments[n];break;}
    el.classList.toggle("active",key===k);
    if(el.dataset.pose===state.pose&&el.dataset.expression===state.expression)continue;
    el.dataset.pose=state.pose;
    el.dataset.expression=state.expression;
    el.innerHTML=speaker.svgs[state.pose+":"+state.expression]||speaker.svgs["idle:idle"];
  }
  if(seen.has(k))return;seen.add(k);
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
