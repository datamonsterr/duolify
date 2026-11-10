import type { LessonCastSnapshot, SlideManifest, Expression } from "./types";
import { compileRigSvg, getBundledFallbackSvg } from "./rig";

function escapeHtml(str: string): string {
  return str
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}

export interface MotionSlideOptions {
  reducedMotion?: boolean;
  offline?: boolean;
}

export function generateMotionSlideHtml(
  slide: SlideManifest,
  cast: LessonCastSnapshot,
  options: MotionSlideOptions = {},
): string {
  const reduced = options.reducedMotion ?? false;
  const offline = options.offline ?? false;

  const guideSpeaker = cast.speakers["guide"];
  const supporterSpeaker = cast.speakers["supporter"];

  const expressions: Expression[] = [
    "idle",
    "happy",
    "sad",
    "thinking",
    "celebrate",
  ];

  const guideSvgs: Record<string, string> = {};
  const supporterSvgs: Record<string, string> = {};

  if (guideSpeaker) {
    for (const exp of expressions) {
      guideSvgs[exp] = compileRigSvg(guideSpeaker.spec, exp);
    }
  }

  if (supporterSpeaker) {
    for (const exp of expressions) {
      supporterSvgs[exp] = compileRigSvg(supporterSpeaker.spec, exp);
    }
  }

  const fallbackSvg = getBundledFallbackSvg("idle");

  const initialSegment = slide.segments[0] || {
    speaker_key: "guide",
    speaker_label: guideSpeaker?.spec.info.name ?? "Guide",
    text: "Welcome to this lesson.",
    expression: "happy",
    start_ms: 0,
    duration_ms: 3000,
  };

  const segmentsJson = JSON.stringify(slide.segments);
  const guideSvgsJson = JSON.stringify(guideSvgs);
  const supporterSvgsJson = JSON.stringify(supporterSvgs);

  return `<!doctype html>
<html>
  <head>
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'"/>
    <meta name="viewport" content="width=device-width, initial-scale=1.0"/>
    <style>
      :root {
        --color-bg: #0F2A3D;
        --color-card: #183C54;
        --color-card-border: #235475;
        --color-text-main: #FFFFFF;
        --color-text-sub: #DDF1FF;
        --color-accent: #1677C9;
      }
      :root[data-motion="reduce"] *,
      :root[data-motion="reduce"] *::before,
      :root[data-motion="reduce"] *::after {
        animation: none !important;
        transition: none !important;
        transform: none !important;
        scroll-behavior: auto !important;
      }
      @media (prefers-reduced-motion: reduce) {
        *, *::before, *::after {
          animation: none !important;
          transition: none !important;
          transform: none !important;
          scroll-behavior: auto !important;
        }
      }
      body {
        margin: 0;
        padding: 16px;
        box-sizing: border-box;
        background: var(--color-bg);
        color: var(--color-text-main);
        font-family: system-ui, -apple-system, sans-serif;
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        min-height: 100vh;
        overflow: hidden;
      }
      .stage-card {
        width: 100%;
        max-width: 600px;
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 16px;
      }
      .character-stage {
        display: flex;
        justify-content: center;
        align-items: center;
        gap: 24px;
        height: 160px;
        width: 100%;
      }
      .speaker-avatar {
        display: flex;
        flex-direction: column;
        align-items: center;
        transition: transform 0.3s ease, opacity 0.3s ease;
        opacity: 0.5;
        transform: scale(0.9);
      }
      .speaker-avatar.active {
        opacity: 1;
        transform: scale(1.05);
      }
      .speaker-avatar svg {
        width: 130px;
        height: 130px;
      }
      .dialogue-card {
        background: var(--color-card);
        border: 2px solid var(--color-card-border);
        border-radius: 16px;
        padding: 16px 20px;
        width: 100%;
        box-sizing: border-box;
        display: flex;
        flex-direction: column;
        gap: 8px;
        box-shadow: 0 4px 12px rgba(0,0,0,0.25);
      }
      .speaker-label {
        font-size: 15px;
        font-weight: 800;
        color: #FFD23F;
        letter-spacing: 0.5px;
        text-transform: uppercase;
      }
      .dialogue-text {
        font-size: 19px;
        font-weight: 600;
        line-height: 1.4;
        color: var(--color-text-sub);
      }
      .expression-nav {
        display: flex;
        gap: 6px;
        margin-top: 8px;
        justify-content: center;
      }
      .expression-nav button {
        background: transparent;
        border: 1px solid var(--color-card-border);
        color: var(--color-text-sub);
        border-radius: 8px;
        padding: 4px 8px;
        font-size: 12px;
        cursor: pointer;
      }
    </style>
  </head>
  <body data-motion="${reduced ? "reduce" : "full"}">
    <div class="stage-card">
      <div class="character-stage">
        <div id="speaker-guide" class="speaker-avatar ${initialSegment.speaker_key === "guide" ? "active" : ""}" data-speaker-key="guide" data-expression="${escapeHtml(initialSegment.expression)}">
          <div id="guide-svg-container">${guideSvgs[initialSegment.expression] || guideSvgs["happy"] || fallbackSvg}</div>
        </div>
        <div id="speaker-supporter" class="speaker-avatar ${initialSegment.speaker_key === "supporter" ? "active" : ""}" data-speaker-key="supporter" data-expression="thinking" style="${supporterSpeaker ? "" : "display:none;"}">
          <div id="supporter-svg-container">${supporterSvgs["thinking"] || supporterSvgs["idle"] || fallbackSvg}</div>
        </div>
      </div>
      <div class="dialogue-card">
        <div id="speaker-label" class="speaker-label">${escapeHtml(initialSegment.speaker_label)}</div>
        <div id="dialogue-text" class="dialogue-text">${escapeHtml(initialSegment.text)}</div>
      </div>
      <div class="expression-nav">
        <button type="button" data-expression-target="idle" onclick="setSpeakerExpression('guide', 'idle')">idle</button>
        <button type="button" data-expression-target="happy" onclick="setSpeakerExpression('guide', 'happy')">happy</button>
        <button type="button" data-expression-target="sad" onclick="setSpeakerExpression('guide', 'sad')">sad</button>
        <button type="button" data-expression-target="thinking" onclick="setSpeakerExpression('guide', 'thinking')">thinking</button>
        <button type="button" data-expression-target="celebrate" onclick="setSpeakerExpression('guide', 'celebrate')">celebrate</button>
      </div>
    </div>
    <script>
      const segments = ${segmentsJson};
      const guideSvgs = ${guideSvgsJson};
      const supporterSvgs = ${supporterSvgsJson};
      const fallbackSvg = ${JSON.stringify(fallbackSvg)};
      const offline = ${offline ? "true" : "false"};

      let currentSegmentIndex = 0;
      let currentTime = 0;
      let isPlaying = true;
      let playbackRate = 1;

      const guideContainer = document.getElementById("guide-svg-container");
      const supporterContainer = document.getElementById("supporter-svg-container");
      const speakerGuide = document.getElementById("speaker-guide");
      const speakerSupporter = document.getElementById("speaker-supporter");
      const speakerLabel = document.getElementById("speaker-label");
      const dialogueText = document.getElementById("dialogue-text");

      function updateSegment(idx) {
        if (!segments[idx]) return;
        currentSegmentIndex = idx;
        const seg = segments[idx];

        speakerLabel.textContent = seg.speaker_label;
        dialogueText.textContent = seg.text;

        if (seg.speaker_key === "guide") {
          speakerGuide.classList.add("active");
          speakerSupporter.classList.remove("active");
          setSpeakerExpression("guide", seg.expression);
        } else {
          speakerSupporter.classList.add("active");
          speakerGuide.classList.remove("active");
          setSpeakerExpression("supporter", seg.expression);
        }

        try {
          window.parent.postMessage({
            type: "character_rendered",
            speaker_key: seg.speaker_key,
            character_version: 1,
            rig_version: "original-v1",
            expression: seg.expression,
            offline: offline,
            slide_id: "${escapeHtml(slide.slide_id)}"
          }, "*");
        } catch(e) {}
      }

      function setSpeakerExpression(speaker, exp) {
        if (speaker === "guide") {
          speakerGuide.dataset.expression = exp;
          if (guideSvgs[exp]) {
            guideContainer.innerHTML = guideSvgs[exp];
          }
        } else if (speaker === "supporter") {
          speakerSupporter.dataset.expression = exp;
          if (supporterSvgs[exp]) {
            supporterContainer.innerHTML = supporterSvgs[exp];
          }
        }
      }

      window.addEventListener("message", (e) => {
        if (!e.data || typeof e.data !== "object") return;

        if (e.data.type === "set_motion" && typeof e.data.reduced === "boolean") {
          document.documentElement.dataset.motion = e.data.reduced ? "reduce" : "full";
          document.body.dataset.motion = e.data.reduced ? "reduce" : "full";
          return;
        }

        if (e.data.type === "set_expression") {
          const spk = e.data.speaker || "guide";
          const exp = e.data.expression || "idle";
          setSpeakerExpression(spk, exp);
          return;
        }

        if (e.data.type === "seek" && typeof e.data.time === "number") {
          currentTime = e.data.time;
          // Determine which segment is active at this time
          let targetIdx = 0;
          for (let i = 0; i < segments.length; i++) {
            if (currentTime >= segments[i].start_ms) {
              targetIdx = i;
            }
          }
          updateSegment(targetIdx);
          return;
        }

        if (e.data.type === "set_missing_rig_test") {
          guideContainer.innerHTML = fallbackSvg;
          try {
            window.parent.postMessage({
              type: "character_render_fallback",
              reason_code: "missing_rig",
              offline: offline
            }, "*");
          } catch(err) {}
          return;
        }

        if (e.data.type === "pause") {
          isPlaying = false;
        }
        if (e.data.type === "play") {
          isPlaying = true;
        }
        if (e.data.type === "rate" && typeof e.data.rate === "number") {
          playbackRate = e.data.rate;
        }
      });

      window.addEventListener("keydown", (e) => {
        if (
          e.code === "Space" ||
          e.key === " " ||
          e.code === "ArrowRight" ||
          e.code === "ArrowLeft"
        ) {
          e.preventDefault();
          try {
            window.parent.postMessage(
              { type: "player_keydown", code: e.code, key: e.key },
              "*",
            );
          } catch (err) {}
        }
      });

      // Initial render notification
      try {
        window.parent.postMessage({
          type: "character_rendered",
          speaker_key: initialSegment.speaker_key,
          character_version: 1,
          rig_version: "original-v1",
          expression: initialSegment.expression,
          offline: offline,
          slide_id: "${escapeHtml(slide.slide_id)}"
        }, "*");
      } catch(e) {}
    </script>
  </body>
</html>`;
}
