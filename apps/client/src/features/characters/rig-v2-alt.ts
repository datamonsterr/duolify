import type { CharacterSpecV2, Styled } from "./types-v2";

export type AltLang = "en" | "ar";

const words = (id: string) => id.replace(/_/g, " ");

/** Integer-only colour naming; mirrors colorNameV2 in alt_v2.go. */
export function colorName(hex: string): string {
  const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16)) as [
    number,
    number,
    number,
  ];
  const mx = Math.max(r, g, b);
  const mn = Math.min(r, g, b);
  const l = Math.trunc((mx + mn) / 2);
  const d = mx - mn;
  if (d < 28) return l < 60 ? "black" : l > 200 ? "white" : "gray";
  let h =
    mx === r
      ? Math.trunc((60 * (g - b)) / d)
      : mx === g
        ? 120 + Math.trunc((60 * (b - r)) / d)
        : 240 + Math.trunc((60 * (r - g)) / d);
  h = ((h % 360) + 360) % 360;
  if (h < 15 || h >= 335) return l > 190 ? "pink" : "red";
  if (h < 70) return l < 110 ? "brown" : h < 45 ? "orange" : "yellow";
  if (h < 160) return "green";
  if (h < 200) return "teal";
  if (h < 255) return "blue";
  if (h < 290) return "purple";
  return "pink";
}

export const skinBand = (tone: string) => {
  const n = parseInt(tone.slice(1), 10);
  return n <= 5 ? "light" : n <= 10 ? "medium" : "deep";
};

// Arabic covers the template skeleton, kinds, ages, skin bands and colours;
// per-part style ids stay as catalog words until the art review fixes names.
const AR: Record<string, string> = {
  black: "أسود",
  white: "أبيض",
  gray: "رمادي",
  pink: "وردي",
  red: "أحمر",
  brown: "بني",
  orange: "برتقالي",
  yellow: "أصفر",
  green: "أخضر",
  teal: "أزرق مخضر",
  blue: "أزرق",
  purple: "بنفسجي",
  light: "فاتحة",
  medium: "متوسطة",
  deep: "داكنة",
  child: "طفل",
  teen: "مراهق",
  adult: "بالغ",
  elder: "كبير السن",
  robot: "روبوت",
  eyes: "عينان",
  glasses: "نظارات",
};
const arWord = (w: string) => AR[w] ?? w;

export function altText(spec: CharacterSpecV2, lang: AltLang = "en"): string {
  const b = spec.body;
  const t = (w: string) => (lang === "ar" ? arWord(w) : w);
  const kind =
    b.kind === "human"
      ? lang === "ar"
        ? `إنسان ببشرة ${t(skinBand(b.skin_tone))}`
        : `human with ${skinBand(b.skin_tone)} skin tone`
      : b.kind === "robot"
        ? lang === "ar"
          ? `روبوت ${t(colorName(b.primary_color))}`
          : `${colorName(b.primary_color)} robot`
        : `${t(colorName(b.primary_color))} ${words(b.species)}`;
  const parts = [`${spec.info.name}, ${t(b.age_band)} ${kind}`];
  if (b.kind === "human") {
    if (b.hair.length === "bald") parts.push("bald");
    else {
      let hair = `${b.hair.length} ${b.hair.texture} ${colorName(b.hair.color)} hair`;
      if (b.hair.style !== "loose") hair += ` in ${words(b.hair.style)}`;
      parts.push(hair);
    }
  }
  parts.push(
    lang === "ar"
      ? `${t("eyes")} ${t(colorName(b.eyes.color))}`
      : `${colorName(b.eyes.color)} eyes`,
  );
  if (b.kind === "human" && b.facial_hair.style !== "none")
    parts.push(words(b.facial_hair.style));
  if (b.glasses.style !== "none")
    parts.push(`${words(b.glasses.style)} ${t("glasses")}`);
  if (b.kind === "human" && b.head_covering.style !== "none")
    parts.push(
      `${colorName(b.head_covering.color)} ${words(b.head_covering.style)}`,
    );
  const worn: string[] = [];
  const c = spec.clothes;
  for (const item of [c.top, c.bottom, c.outerwear, c.shoes] as Styled[])
    if (item.style !== "none")
      worn.push(`a ${colorName(item.color)} ${words(item.style)}`);
  for (const a of c.accessories) worn.push(`a ${words(a.kind)}`);
  let text = parts.join(lang === "ar" ? "، " : ", ");
  if (worn.length) {
    const lead = lang === "ar" ? "يرتدي" : "wearing";
    const last = worn[worn.length - 1]!;
    const head = worn.slice(0, -1);
    text += `${lang === "ar" ? "، " : ", "}${lead} ${head.length ? `${head.join(", ")} ${lang === "ar" ? "و" : "and"} ` : ""}${last}`;
  }
  return `${text}.`;
}
