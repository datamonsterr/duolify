import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UiPreferencesProvider } from "../design/localization/Preferences";
import { Hero, Landing, LandingVideo, Pillars, Steps, TopBar } from "./Landing";

const capture = vi.hoisted(() => vi.fn());
vi.mock("../../lib/posthog", () => ({
  posthog: { capture, has_opted_out_capturing: () => false },
}));
const media = vi.hoisted(() => ({
  mp4: "https://media.test/landing/v1/landing.mp4",
  webm: "https://media.test/landing/v1/landing.webm",
  poster: "/landing/landing-poster-960.webp",
  captionsEn: "/landing/landing.en.vtt",
}));
vi.mock("./config", () => ({ landingMedia: media }));
vi.mock("../design/brand/Brand", () => ({ Brand: () => <span>Duolify</span> }));

function wrap(node: React.ReactNode, locale = "en") {
  return render(
    <UiPreferencesProvider initialPreferences={{ locale: locale as "en" }}>
      {node}
    </UiPreferencesProvider>,
  );
}
beforeEach(() => {
  capture.mockClear();
  localStorage.clear();
});

describe("landing sections", () => {
  it("top bar offers language, theme and Sign in", () => {
    const onSignIn = vi.fn();
    wrap(<TopBar onSignIn={onSignIn} />);
    expect(screen.getByLabelText("Language")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Theme" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(onSignIn).toHaveBeenCalled();
  });
  it("hero has one H1, a primary action and a shared-course link", () => {
    const start = vi.fn();
    const shared = vi.fn();
    wrap(<Hero onGetStarted={start} onTryShared={shared} />);
    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Learn anything. Like a game.",
      }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Get started" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Try a shared course" }),
    );
    expect(start).toHaveBeenCalled();
    expect(shared).toHaveBeenCalled();
  });
  it("video never autoplays, defers loading and shows controls after play", () => {
    const { container } = wrap(<LandingVideo />);
    const video = container.querySelector("video")!;
    expect(video.getAttribute("preload")).toBe("none");
    expect(video.hasAttribute("autoplay")).toBe(false);
    expect(video.hasAttribute("controls")).toBe(false);
    expect(video.querySelector("track")?.getAttribute("srclang")).toBe("en");
    HTMLMediaElement.prototype.play = vi.fn().mockResolvedValue(undefined);
    fireEvent.click(screen.getByRole("button", { name: "Play video" }));
    expect(video.hasAttribute("controls")).toBe(true);
    fireEvent.play(video);
    fireEvent.ended(video);
    expect(capture.mock.calls.map((c) => c[0])).toEqual([
      "landing_video_played",
      "landing_video_completed",
    ]);
  });
  it("without a media base shows the poster and no player", () => {
    const saved = { ...media };
    Object.assign(media, { mp4: "", webm: "" });
    const { container } = wrap(<LandingVideo />);
    expect(screen.queryByRole("button", { name: "Play video" })).toBeNull();
    expect(container.querySelector("source")).toBeNull();
    expect(container.querySelector("video")?.getAttribute("poster")).toMatch(
      /poster/,
    );
    Object.assign(media, saved);
  });
  it("a failing source drops the player and keeps the poster", () => {
    const { container } = wrap(<LandingVideo />);
    HTMLMediaElement.prototype.play = vi.fn().mockResolvedValue(undefined);
    fireEvent.click(screen.getByRole("button", { name: "Play video" }));
    fireEvent.error(container.querySelector("source")!);
    expect(container.querySelector("video")?.hasAttribute("controls")).toBe(
      false,
    );
    expect(container.querySelector("source")).toBeNull();
  });
  it("pillars and steps carry the agreed copy", () => {
    wrap(
      <>
        <Pillars />
        <Steps />
      </>,
    );
    for (const text of [
      "Play to learn",
      "Interactive video",
      "Your course, by AI",
      "Say what you want to learn",
      "Get your course in minutes",
      "Play 5 minutes a day",
    ])
      expect(screen.getByText(text)).toBeTruthy();
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
  });
  it("renders in Arabic", () => {
    wrap(<Pillars />, "ar");
    expect(screen.getByText("العب لتتعلم")).toBeTruthy();
  });
  it("sends anonymous events and nothing after opt-out", () => {
    const view = wrap(
      <Landing
        welcome={<p>welcome</p>}
        footer={<p>footer</p>}
        onOpen={() => {}}
        onTryShared={() => {}}
      />,
    );
    fireEvent.click(screen.getAllByRole("button", { name: "Get started" })[0]);
    const events = capture.mock.calls.map((c) => [c[0], c[1].target]);
    expect(events).toContainEqual(["landing_viewed", undefined]);
    expect(events).toContainEqual(["landing_cta_clicked", "hero_get_started"]);
    expect(JSON.stringify(capture.mock.calls)).not.toMatch(/distinct_id/);
    view.unmount();
    capture.mockClear();
    localStorage.setItem("duolify.analytics-opt-out", "true");
    wrap(<Hero onGetStarted={() => {}} onTryShared={() => {}} />);
    fireEvent.play(document.querySelector("video")!);
    expect(capture).not.toHaveBeenCalled();
  });
});
