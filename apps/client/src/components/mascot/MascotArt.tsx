import { useEffect, useRef, useState, type CSSProperties } from "react";
import type { MessageId } from "../../features/design/localization/messages";
import { useUiPreferences } from "../../features/design/localization/Preferences";
import {
  MASCOT_SWATCH_COLORS,
  type MascotAccessory,
  type MascotState,
  type MascotSwatch,
} from "./rig-part-spec";
import "./mascot.css";

export type MascotAppearance = {
  swatch?: MascotSwatch;
  accessories?: readonly MascotAccessory[];
};
export type MascotProps = {
  state?: MascotState;
  /** Rendered width in px. */
  size?: number;
  /** `bust` (head and antenna) stays crisp at 48 px and below. Default: bust under 56 px. */
  view?: "full" | "bust";
  /** Accessible name when the state conveys information; otherwise the art is aria-hidden. */
  label?: string;
  /** Use the localized state name as the accessible name. */
  labelled?: boolean;
  appearance?: MascotAppearance;
  /** Eye direction, each axis -1..1 (for example toward the composer). */
  gaze?: { x: number; y: number };
  /** Increase once per streamed text chunk to sync the mouth. Omit for a steady loop. */
  chunk?: number;
  /** Adds the slashed-signal glyph (use with `sleepy`). */
  offline?: boolean;
  className?: string;
};

const VIEW = { full: "-10 -4 220 222", bust: "20 0 160 152" } as const;
const RATIO = { full: 222 / 220, bust: 152 / 160 } as const;

const Eyes = ({ s }: { s: MascotState }) => {
  const arcs = s === "happy" || s === "celebrate";
  if (arcs)
    return (
      <>
        <path d="M68 88 Q78.5 68 89 88" />
        <path d="M111 88 Q121.5 68 132 88" />
      </>
    );
  if (s === "sleepy")
    return (
      <>
        <path d="M68 82 Q78.5 91 89 82" />
        <path d="M111 82 Q121.5 91 132 82" />
      </>
    );
  const [lw, lh, ly, rw, rh, ry] =
    s === "listening"
      ? [19, 30, 67, 19, 30, 67]
      : s === "confused"
        ? [17, 17, 76, 19, 30, 66]
        : [17, 26, 70, 17, 26, 70];
  const lx = 78.5 - lw / 2,
    rx = 121.5 - rw / 2;
  return (
    <g className="m-fill">
      <rect x={lx} y={ly} width={lw} height={lh} rx={lw / 2} />
      <rect x={rx} y={ry} width={rw} height={rh} rx={rw / 2} />
      <circle className="m-glint" cx={lx + 5} cy={ly + 7.5} r="3" />
      <circle className="m-glint" cx={rx + 5} cy={ry + 7.5} r="3" />
    </g>
  );
};

const Mouth = ({
  s,
  loop,
  r,
}: {
  s: MascotState;
  loop: boolean;
  r: React.Ref<SVGGElement>;
}) => {
  if (s === "talking")
    return (
      <g ref={r} className={`m-mouth m-fill${loop ? " m-loop" : ""}`}>
        <rect x="92" y="101" width="16" height="13" rx="6.5" />
      </g>
    );
  if (s === "celebrate")
    return (
      <path className="m-fill" d="M87 101 h26 q0 16 -13 16 q-13 0 -13 -16z" />
    );
  const d =
    s === "happy"
      ? "M87 104 q13 13 26 0"
      : s === "confused"
        ? "M89 109 q5.5 -7 11 0 t11 0"
        : s === "sleepy"
          ? "M95 108 q5 4 10 0"
          : s === "thinking"
            ? "M99 108 h13"
            : s === "listening"
              ? "M94 108 h12"
              : "M90 105 q10 9 20 0";
  return <path className="m-line" d={d} />;
};

const Glyph = ({ s }: { s: MascotState }) => {
  if (s === "thinking")
    return (
      <g className="m-acc">
        {[0, 1, 2].map((i) => (
          <circle
            key={i}
            className="m-dot"
            style={{ "--i": i } as CSSProperties}
            cx={130 + i * 14}
            cy={30 - i * 4}
            r="4.5"
          />
        ))}
      </g>
    );
  if (s === "confused")
    return (
      <g className="m-glyph m-acc-s">
        <path d="M148 34 q0 -12 11 -12 q11 0 11 9 q0 6 -11 10 v5" />
        <circle className="m-acc" cx="159" cy="55" r="3.2" />
      </g>
    );
  if (s === "sleepy")
    return (
      <g className="m-acc-s">
        <path className="m-z" d="M146 40 h12 l-12 13 h12" />
        <path className="m-z m-z2" d="M164 22 h8 l-8 9 h8" />
      </g>
    );
  return null;
};

const Confetti = () => (
  <g className="m-confetti">
    {[
      [-78, -22, -40, "a"],
      [-52, -58, 25, "b"],
      [-18, -76, 70, "c"],
      [20, -80, -20, "d"],
      [56, -60, 40, "a"],
      [82, -24, -60, "b"],
      [-90, 18, 15, "c"],
      [92, 14, 55, "d"],
    ].map(([x, y, r, c], i) => (
      <rect
        key={i}
        className={`m-cf m-cf-${c}`}
        style={
          {
            "--dx": `${x}px`,
            "--dy": `${y}px`,
            "--r": `${r}deg`,
            "--n": i,
          } as CSSProperties
        }
        x="96"
        y="58"
        width="7"
        height="11"
        rx="2.5"
      />
    ))}
  </g>
);

const Offline = () => (
  <g transform="translate(160 22)">
    <circle r="15" className="m-badge" />
    <path className="m-sig" d="M-8 -3 q8 -7 16 0 M-4 2 q4 -3.500 8 0" />
    <circle className="m-sigdot" cy="7" r="1.8" />
    <path className="m-slash" d="M-9 -9 L9 9" />
  </g>
);

const Accessory = ({ a }: { a: MascotAccessory }) => {
  if (a === "headphones")
    return (
      <g className="m-hp">
        <path d="M43 84 Q43 32 100 32 Q157 32 157 84" className="m-hp-band" />
        <rect x="27" y="70" width="19" height="34" rx="9" />
        <rect x="154" y="70" width="19" height="34" rx="9" />
      </g>
    );
  if (a === "sprout")
    return (
      <g className="m-sprout">
        <path d="M72 46 q-1 -10 4 -16" className="m-sprout-stem" />
        <path d="M76 30 q-14 -2 -14 -14 q13 0 14 14z M76 30 q12 -1 13 -13 q-12 -1 -13 13z" />
      </g>
    );
  return null;
};

export function MascotArt({
  state = "idle",
  size = 96,
  view,
  label: labelProp,
  labelled,
  appearance,
  gaze,
  chunk,
  offline,
  className,
}: MascotProps) {
  const v = view ?? (size < 56 ? "bust" : "full");
  const { reducedMotion, t } = useUiPreferences();
  const label =
    labelProp ??
    (labelled ? t(`mascot.state.${state}` as MessageId) : undefined);
  const [system, setSystem] = useState(
    () =>
      typeof matchMedia === "function" &&
      matchMedia("(prefers-reduced-motion: reduce)").matches,
  );
  useEffect(() => {
    if (typeof matchMedia !== "function") return;
    const m = matchMedia("(prefers-reduced-motion: reduce)");
    const on = () => setSystem(m.matches);
    m.addEventListener?.("change", on);
    return () => m.removeEventListener?.("change", on);
  }, []);
  const still = reducedMotion || system;
  const root = useRef<SVGSVGElement>(null);
  const eyes = useRef<SVGGElement>(null);
  const mouth = useRef<SVGGElement>(null);
  const [visible, setVisible] = useState(true);
  const [tabHidden, setTabHidden] = useState(
    typeof document !== "undefined" && document.visibilityState === "hidden",
  );
  const paused = !visible || tabHidden;

  useEffect(() => {
    const el = root.current;
    if (!el || typeof IntersectionObserver !== "function") return;
    const io = new IntersectionObserver((e) =>
      setVisible(e.some((x) => x.isIntersecting)),
    );
    io.observe(el);
    return () => io.disconnect();
  }, []);
  useEffect(() => {
    const on = () => setTabHidden(document.visibilityState === "hidden");
    document.addEventListener("visibilitychange", on);
    return () => document.removeEventListener("visibilitychange", on);
  }, []);

  // Blink at natural, random intervals (sometimes a quick double blink).
  const blinks =
    state !== "happy" && state !== "celebrate" && state !== "sleepy";
  useEffect(() => {
    if (still || paused || !blinks) return;
    let id: ReturnType<typeof setTimeout>;
    const blink = (double: boolean) => {
      eyes.current?.animate?.(
        [
          { transform: "scaleY(1)" },
          { transform: "scaleY(.08)", offset: 0.45 },
          { transform: "scaleY(1)" },
        ],
        { duration: 150, easing: "ease-in-out", iterations: double ? 2 : 1 },
      );
      id = setTimeout(
        () => blink(Math.random() < 0.18),
        2200 + Math.random() * 3800,
      );
    };
    id = setTimeout(() => blink(false), 1200 + Math.random() * 2400);
    return () => clearTimeout(id);
  }, [still, paused, blinks]);

  // Mouth follows each streamed chunk.
  useEffect(() => {
    if (still || paused || chunk === undefined || state !== "talking") return;
    mouth.current?.animate?.(
      [
        { transform: "scaleY(.35)" },
        { transform: "scaleY(1.15)", offset: 0.4 },
        { transform: "scaleY(.55)" },
      ],
      { duration: 160, easing: "ease-out" },
    );
  }, [chunk, still, paused, state]);

  const sw = appearance?.swatch
    ? MASCOT_SWATCH_COLORS[appearance.swatch]
    : undefined;
  const style = {
    width: size,
    height: size * RATIO[v],
    ...(sw && {
      "--m-body": sw.body,
      "--m-body-edge": sw.bodyEdge,
      "--m-trim": sw.trim,
      "--m-trim-edge": sw.trim,
      "--m-eye": sw.eye,
    }),
    ...(gaze && {
      "--gx": `${Math.max(-1, Math.min(1, gaze.x)) * 5}px`,
      "--gy": `${Math.max(-1, Math.min(1, gaze.y)) * 4}px`,
    }),
  } as CSSProperties;
  const acc = appearance?.accessories ?? [];

  return (
    <svg
      ref={root}
      className={`m${className ? ` ${className}` : ""}`}
      viewBox={VIEW[v]}
      style={style}
      data-state={state}
      data-view={v}
      data-motion={still ? "off" : "on"}
      data-paused={paused ? "true" : "false"}
      focusable="false"
      {...(label
        ? { role: "img", "aria-label": label }
        : { "aria-hidden": true })}
    >
      <ellipse className="m-shadow" cx="100" cy="207" rx="44" ry="6" />
      <g className="m-bob">
        <g className="m-arm m-arm-l">
          <path className="m-edge" d="M66 155 L45 175" />
          <path className="m-trim" d="M66 151 L45 171" />
        </g>
        <g className="m-arm m-arm-r">
          <path className="m-edge" d="M134 155 L155 175" />
          <path className="m-trim" d="M134 151 L155 171" />
        </g>
        <g className="m-body">
          <rect
            className="m-edge"
            x="64"
            y="136"
            width="72"
            height="62"
            rx="28"
          />
          <rect
            className="m-main"
            x="64"
            y="132"
            width="72"
            height="62"
            rx="28"
          />
          <circle className="m-trim" cx="100" cy="162" r="9" />
          <circle className="m-acc m-core" cx="100" cy="162" r="5" />
          {acc.includes("scarf") && (
            <g className="m-scarf">
              <rect x="70" y="138" width="60" height="12" rx="6" />
              <rect x="112" y="146" width="12" height="22" rx="5" />
            </g>
          )}
          {acc.includes("bowtie") && (
            <g className="m-bow">
              <path d="M100 148 L80 140 V156z M100 148 L120 140 V156z" />
              <circle cx="100" cy="148" r="5" />
            </g>
          )}
          {acc.includes("badge") && (
            <path
              className="m-acc"
              d="M84 170 l3.5 7 7.5 1 -5.5 5 1.5 7.5 -7 -3.8 -7 3.8 1.5 -7.5 -5.5 -5 7.5 -1z"
            />
          )}
        </g>
        <g className="m-head">
          <g className="m-ant">
            <path className="m-trim m-stem" d="M100 46 V25" />
            <circle className="m-halo" cx="100" cy="17" r="15" />
            <circle className="m-bulb" cx="100" cy="17" r="9" />
          </g>
          <circle className="m-trim" cx="38" cy="88" r="13" />
          <circle className="m-trim" cx="162" cy="88" r="13" />
          {state === "listening" && (
            <g className="m-rings">
              <circle cx="30" cy="88" r="17" />
              <circle cx="170" cy="88" r="17" />
            </g>
          )}
          <rect
            className="m-edge"
            x="38"
            y="48"
            width="124"
            height="94"
            rx="42"
          />
          <rect
            className="m-main"
            x="38"
            y="42"
            width="124"
            height="94"
            rx="42"
          />
          <path className="m-shine" d="M60 52 q14 -8 30 -8" />
          <rect
            className="m-visor"
            x="52"
            y="56"
            width="96"
            height="66"
            rx="30"
          />
          <path className="m-reflect" d="M118 58 h14 l-22 62 h-14z" />
          <g className="m-gaze">
            <g ref={eyes} className="m-eyes m-stroke-eye">
              <Eyes s={state} />
            </g>
          </g>
          {(state === "happy" || state === "celebrate") && (
            <g className="m-cheek">
              <circle cx="63" cy="105" r="6" />
              <circle cx="137" cy="105" r="6" />
            </g>
          )}
          <g className="m-mouth-wrap m-stroke-eye">
            <Mouth s={state} loop={chunk === undefined} r={mouth} />
          </g>
          {acc
            .filter((a) => a === "headphones" || a === "sprout")
            .map((a) => (
              <Accessory key={a} a={a} />
            ))}
          <Glyph s={state} />
        </g>
        {state === "celebrate" && <Confetti />}
      </g>
      {offline && <Offline />}
    </svg>
  );
}
