import {
  act,
  render as rtlRender,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { UiPreferencesProvider } from "../../features/design/localization/Preferences";
import {
  Mascot,
  MascotAvatar,
  PlayerCompanion,
  emitMascotTalk,
  setMascotPreference,
  DEFAULT_MASCOT_PREFERENCE,
} from "./index";
import { readFileSync } from "node:fs";

const css = readFileSync(
  `${process.cwd()}/src/components/mascot/mascot.css`,
  "utf8",
)
  .replace(/\s+/g, "")
  .replace(/\/\*.*?\*\//g, "");

const render = async (ui: React.ReactElement) => {
  const r = rtlRender(ui);
  await waitFor(() => expect(r.container.querySelector("svg")).not.toBeNull());
  return r;
};
const svg = (c: HTMLElement) => c.querySelector("svg")!;

describe("Mascot", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    setMascotPreference(DEFAULT_MASCOT_PREFERENCE);
  });
  it("is aria-hidden when decorative and named when it conveys state", async () => {
    const { container, rerender } = await render(<Mascot state="happy" />);
    expect(svg(container)).toHaveAttribute("aria-hidden", "true");
    rerender(<Mascot state="thinking" labelled />);
    expect(
      screen.getByRole("img", { name: "Buo is thinking" }),
    ).toBeInTheDocument();
  });
  it("renders each state and size, avatar is decorative", async () => {
    for (const s of [
      "idle",
      "listening",
      "thinking",
      "talking",
      "happy",
      "celebrate",
      "confused",
      "sleepy",
    ] as const) {
      const { container, unmount } = await render(
        <Mascot state={s} size={32} />,
      );
      expect(svg(container)).toHaveAttribute("data-state", s);
      expect(svg(container)).toHaveAttribute("data-view", "bust");
      unmount();
    }
    const { container } = await render(<MascotAvatar size={40} />);
    expect(svg(container)).toHaveAttribute("aria-hidden", "true");
  });
  it("shows static poses under the in-app reduced motion setting", async () => {
    const { container } = await render(
      <UiPreferencesProvider initialPreferences={{ reduceMotion: true }}>
        <Mascot state="celebrate" />
      </UiPreferencesProvider>,
    );
    expect(svg(container)).toHaveAttribute("data-motion", "off");
    expect(css).toMatch(/data-motion="?off"?\]\*\{animation:none!important/);
  });
  it("shows static poses under prefers-reduced-motion", async () => {
    vi.stubGlobal("matchMedia", (q: string) => ({
      matches: q.includes("reduce"),
      addEventListener() {},
      removeEventListener() {},
    }));
    const { container } = await render(<Mascot state="idle" />);
    expect(svg(container)).toHaveAttribute("data-motion", "off");
  });
  it("pauses off-screen and when the tab is hidden", async () => {
    let cb: (e: { isIntersecting: boolean }[]) => void = () => {};
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(c: typeof cb) {
          cb = c;
        }
        observe() {}
        disconnect() {}
      },
    );
    const { container } = await render(<Mascot />);
    expect(svg(container)).toHaveAttribute("data-paused", "false");
    act(() => cb([{ isIntersecting: false }]));
    expect(svg(container)).toHaveAttribute("data-paused", "true");
    act(() => cb([{ isIntersecting: true }]));
    expect(svg(container)).toHaveAttribute("data-paused", "false");
    Object.defineProperty(document, "visibilityState", {
      value: "hidden",
      configurable: true,
    });
    act(() => void document.dispatchEvent(new Event("visibilitychange")));
    expect(svg(container)).toHaveAttribute("data-paused", "true");
    Object.defineProperty(document, "visibilityState", {
      value: "visible",
      configurable: true,
    });
  });
  it("never intercepts pointer events and animates only transform and opacity", async () => {
    expect(css).toMatch(/\.m\{[^}]*pointer-events:none/);
    const props = [
      ...css.matchAll(/@keyframes[^{]+\{((?:[^{}]|\{[^}]*\})*)\}/g),
    ].flatMap((m) => [...m[1].matchAll(/([a-z-]+):/g)].map((x) => x[1]));
    for (const p of new Set(props))
      expect([
        "translate",
        "scale",
        "rotate",
        "opacity",
        "transform",
      ]).toContain(p);
  });
  it("takes colours only from variables (no literals in shapes)", async () => {
    const { container } = await render(
      <Mascot
        state="celebrate"
        appearance={{ swatch: "teal", accessories: ["headphones", "bowtie"] }}
      />,
    );
    expect(container.innerHTML).not.toMatch(/fill="#|stroke="#/);
    expect(svg(container).style.getPropertyValue("--m-body")).toBe("#1A9B82");
  });
  it("mirrors the mouth to streamed chunks", async () => {
    const animate = vi.fn();
    Element.prototype.animate = animate;
    const { container, rerender } = await render(
      <Mascot state="talking" chunk={1} />,
    );
    expect(container.querySelector(".m-loop")).toBeNull();
    rerender(<Mascot state="talking" chunk={2} />);
    expect(animate).toHaveBeenCalled();
  });
});

describe("PlayerCompanion", () => {
  afterEach(() => setMascotPreference(DEFAULT_MASCOT_PREFERENCE));
  it("is 48 px in the framed player and honours the hide setting", async () => {
    const { container, rerender } = await render(<PlayerCompanion />);
    expect(container.querySelector(".m-companion svg")).toHaveStyle({
      width: "48px",
    });
    act(() =>
      setMascotPreference({
        ...DEFAULT_MASCOT_PREFERENCE,
        companionVisible: false,
      }),
    );
    rerender(<PlayerCompanion />);
    expect(container.querySelector(".m-companion")).toBeNull();
  });
  it("is hidden in immersive mode unless speaking", async () => {
    const { container } = rtlRender(<PlayerCompanion immersive />);
    expect(container.querySelector(".m-companion")).toBeNull();
    act(() => emitMascotTalk(true));
    await waitFor(() =>
      expect(container.querySelector(".m-companion svg")).toHaveAttribute(
        "data-state",
        "talking",
      ),
    );
    act(() => emitMascotTalk(false));
    expect(container.querySelector(".m-companion")).toBeNull();
  });
});

describe("Mascot art chunk failure", () => {
  it("leaves a blank slot and does not throw", async () => {
    vi.resetModules();
    vi.doMock("./MascotArt", () => {
      throw new Error("chunk load failed");
    });
    const { Mascot: Fresh } = await import("./Mascot");
    const err = vi.spyOn(console, "error").mockImplementation(() => {});
    const { container } = rtlRender(<Fresh state="idle" size={48} />);
    await waitFor(() =>
      expect(container.querySelector("span[aria-hidden]")).not.toBeNull(),
    );
    expect(container.querySelector("svg")).toBeNull();
    err.mockRestore();
    vi.doUnmock("./MascotArt");
  });
});
