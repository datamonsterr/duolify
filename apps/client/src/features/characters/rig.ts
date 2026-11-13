import type { CharacterSpec, Expression } from "./types";
import { getDefaultCastFixtures, getBundledFallbackSpec } from "./fixtures";
import { validateCharacterSpec } from "./schema";

export {
  getDefaultCastFixtures,
  getBundledFallbackSpec,
  validateCharacterSpec,
};
export type { CharacterSpec, Expression };

export function compileRigSvg(
  spec: CharacterSpec,
  expression: Expression = "idle",
): string {
  const validation = validateCharacterSpec(spec);
  if (!validation.valid) {
    return getBundledFallbackSvg(expression);
  }

  const { palette, body_shape, eyes, mouth, accessories } = spec.appearance;

  let bodyElement: string;
  switch (body_shape) {
    case "star":
      bodyElement = `<polygon data-part="body-shape" points="80,18 97,55 138,58 107,85 117,126 80,103 43,126 53,85 22,58 63,55" fill="${palette.body}" stroke="${palette.outline}" stroke-width="5" stroke-linejoin="round"/>`;
      break;
    case "bean":
      bodyElement = `<path data-part="body-shape" d="M 50,45 C 50,20 110,20 110,45 C 110,75 125,95 115,125 C 105,145 55,145 45,125 C 35,95 50,75 50,45 Z" fill="${palette.body}" stroke="${palette.outline}" stroke-width="5" stroke-linejoin="round"/>`;
      break;
    case "round":
    default:
      bodyElement = `<circle data-part="body-shape" cx="80" cy="80" r="54" fill="${palette.body}" stroke="${palette.outline}" stroke-width="5"/>`;
      break;
  }

  let accessoriesPre = "";
  let accessoriesPost = "";
  for (const acc of accessories) {
    switch (acc) {
      case "hat":
        accessoriesPre += `<path data-part="accessory-hat" d="M 55,38 L 80,12 L 105,38 Z" fill="${palette.accent}" stroke="${palette.outline}" stroke-width="3"/>`;
        break;
      case "scarf":
        accessoriesPre += `<path data-part="accessory-scarf" d="M 46,108 Q 80,120 114,108 Q 118,124 100,128 L 104,146 L 88,146 L 86,128 Q 42,124 46,108 Z" fill="${palette.accent}" stroke="${palette.outline}" stroke-width="3"/>`;
        break;
      case "badge":
        accessoriesPre += `<circle data-part="accessory-badge" cx="112" cy="106" r="9" fill="${palette.accent}" stroke="${palette.outline}" stroke-width="2"/>`;
        break;
      case "glasses":
        accessoriesPost += `<g data-part="accessory-glasses" stroke="${palette.outline}" stroke-width="3" fill="none"><circle cx="66" cy="68" r="14"/><circle cx="94" cy="68" r="14"/><line x1="80" y1="68" x2="80" y2="68"/></g>`;
        break;
    }
  }

  let eyesElement: string;
  switch (expression) {
    case "happy":
      eyesElement = `<path data-part="eyes-happy" d="M 56,72 Q 66,60 76,72 M 84,72 Q 94,60 104,72" fill="none" stroke="${palette.outline}" stroke-width="4" stroke-linecap="round"/>`;
      break;
    case "sad":
      eyesElement = `<path data-part="eyes-sad" d="M 56,66 Q 66,76 76,66 M 84,66 Q 94,76 104,66" fill="none" stroke="${palette.outline}" stroke-width="4" stroke-linecap="round"/>`;
      break;
    case "thinking":
      eyesElement = `<ellipse data-part="eyes-thinking" cx="66" cy="64" rx="5" ry="6" fill="${palette.outline}"/><ellipse cx="94" cy="58" rx="5" ry="6" fill="${palette.outline}"/><path d="M 88,48 Q 96,44 104,49" fill="none" stroke="${palette.outline}" stroke-width="3" stroke-linecap="round"/>`;
      break;
    case "celebrate":
      eyesElement = `<path data-part="eyes-celebrate" d="M 56,70 L 66,62 L 76,70 M 84,70 L 94,62 L 104,70" fill="none" stroke="${palette.outline}" stroke-width="4" stroke-linecap="round"/>`;
      break;
    case "idle":
    default:
      switch (eyes) {
        case "oval":
          eyesElement = `<ellipse data-part="eyes-idle" cx="66" cy="68" rx="5" ry="8" fill="${palette.outline}"/><ellipse cx="94" cy="68" rx="5" ry="8" fill="${palette.outline}"/>`;
          break;
        case "sleepy":
          eyesElement = `<path data-part="eyes-idle" d="M 58,70 Q 66,75 74,70 M 86,70 Q 94,75 102,70" fill="none" stroke="${palette.outline}" stroke-width="4" stroke-linecap="round"/>`;
          break;
        case "round":
        default:
          eyesElement = `<circle data-part="eyes-idle" cx="66" cy="68" r="6" fill="${palette.outline}"/><circle cx="94" cy="68" r="6" fill="${palette.outline}"/>`;
          break;
      }
      break;
  }

  let mouthElement: string;
  switch (expression) {
    case "happy":
      mouthElement = `<path data-part="mouth-happy" d="M 64,88 Q 80,112 96,88 Z" fill="${palette.accent}" stroke="${palette.outline}" stroke-width="3"/>`;
      break;
    case "sad":
      mouthElement = `<path data-part="mouth-sad" d="M 68,102 Q 80,90 92,102" fill="none" stroke="${palette.outline}" stroke-width="3" stroke-linecap="round"/>`;
      break;
    case "thinking":
      mouthElement = `<ellipse data-part="mouth-thinking" cx="86" cy="94" rx="4" ry="4" fill="${palette.outline}"/>`;
      break;
    case "celebrate":
      mouthElement = `<ellipse data-part="mouth-celebrate" cx="80" cy="95" rx="14" ry="10" fill="${palette.accent}" stroke="${palette.outline}" stroke-width="3"/>`;
      break;
    case "idle":
    default:
      switch (mouth) {
        case "line":
          mouthElement = `<line data-part="mouth-idle" x1="68" y1="92" x2="92" y2="92" stroke="${palette.outline}" stroke-width="3" stroke-linecap="round"/>`;
          break;
        case "open":
          mouthElement = `<ellipse data-part="mouth-idle" cx="80" cy="92" rx="7" ry="9" fill="${palette.outline}" stroke="${palette.outline}" stroke-width="2"/>`;
          break;
        case "smile":
        default:
          mouthElement = `<path data-part="mouth-idle" d="M 68,88 Q 80,102 92,88" fill="none" stroke="${palette.outline}" stroke-width="3" stroke-linecap="round"/>`;
          break;
      }
      break;
  }

  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 160 160" class="character-rig" data-expression="${expression}">${bodyElement}${accessoriesPre}${eyesElement}${mouthElement}${accessoriesPost}</svg>`;
}

export function getBundledFallbackSvg(expression: Expression = "idle"): string {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 160 160" class="character-rig fallback-character fallback-mascot" data-fallback="true" data-expression="${expression}"><circle data-part="body-shape" cx="80" cy="80" r="50" fill="#90E0EF" stroke="#03045E" stroke-width="4"/><circle data-part="eyes-idle" cx="65" cy="70" r="5" fill="#03045E"/><circle cx="95" cy="70" r="5" fill="#03045E"/><path data-part="mouth-idle" d="M 70 95 Q 80 105 90 95" fill="none" stroke="#03045E" stroke-width="3" stroke-linecap="round"/></svg>`;
}
