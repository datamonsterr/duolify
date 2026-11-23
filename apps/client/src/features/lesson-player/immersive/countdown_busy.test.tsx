import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
}));
vi.mock("../../../lib/analytics", async (orig) => ({
  ...(await orig<object>()),
  trackImmersive: vi.fn(),
}));
import ImmersiveLayer from "./ImmersiveLayer";
import { ImmersiveExtensionsContext } from "./extensions";
import type { ImmersiveActions, ImmersiveView } from "./types";

const view: ImmersiveView = {
  title: "T",
  lessonId: "l",
  courseId: "c",
  index: 0,
  slides: [
    { id: "a", label: "A", durationMs: 1000 },
    { id: "b", label: "B", durationMs: 1000 },
  ],
  playing: false,
  ended: true,
  timeMs: 1000,
  durationMs: 1000,
  rate: 1,
  captions: false,
  muted: false,
  bookmarked: false,
  autoAdvance: true,
  fullscreenOnRotate: false,
  failed: false,
  disabled: false,
  offline: false,
  reducedMotion: true,
  caption: "",
};
const actions = Object.fromEntries(
  [
    "toggle",
    "pause",
    "previous",
    "next",
    "goTo",
    "replay",
    "cycleSpeed",
    "toggleCaptions",
    "toggleMute",
    "toggleBookmark",
    "setAutoAdvance",
    "setFullscreenOnRotate",
    "exit",
  ].map((k) => [k, vi.fn()]),
) as unknown as ImmersiveActions;

describe("end-of-slide countdown", () => {
  it("shows while idle and stays away while a note, question or selection is open", () => {
    const idle = render(
      <ImmersiveLayer
        view={view}
        actions={actions}
        lastSlide={false}
        lessonId="l"
      />,
    );
    expect(screen.queryByTestId("immersive-countdown")).not.toBeNull();
    idle.unmount();
    render(
      <ImmersiveExtensionsContext.Provider value={{ busy: true }}>
        <ImmersiveLayer
          view={view}
          actions={actions}
          lastSlide={false}
          lessonId="l"
        />
      </ImmersiveExtensionsContext.Provider>,
    );
    expect(screen.queryByTestId("immersive-countdown")).toBeNull();
  });
});
