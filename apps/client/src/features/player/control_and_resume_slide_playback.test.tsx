import { describe, expect, it, vi, afterEach } from "vitest";
import posthog from "posthog-js/dist/module.no-external";
import { PlaybackClock } from "./playback";
import {
  validPosition,
  playerReturnPath,
  readLocalPosition,
  writeLocalPosition,
  savePosition,
} from "./position";
import { isSlideMessage } from "../lesson-player/bridge";
import { playerManifest } from "../../mocks/player";
import { configureAnalytics, trackPlayer } from "../../lib/analytics";
import { session } from "../accounts/session";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { LessonPlayer } from "../lesson-player/LessonPlayer";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
  },
}));
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  configureAnalytics(null);
  sessionStorage.clear();
  vi.clearAllMocks();
});
describe("controlled playback clock", () => {
  it("serializes saves, rejects malformed acknowledgments, and cannot recreate a logged-out cursor", async () => {
    const owner = "a1111111-1111-4111-8111-111111111111";
    const current = vi.spyOn(session, "current").mockReturnValue({
      id: owner,
      display_name: "",
      locale: "en",
      time_zone: "UTC",
      background: "",
      analytics_opt_out: true,
    });
    let release!: () => void;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    const times: number[] = [];
    vi.spyOn(session, "request").mockImplementation(
      async (_path, _method, body) => {
        const position = body as { time_ms: number };
        times.push(position.time_ms);
        if (position.time_ms === 100) await pending;
        return body as never;
      },
    );
    const p = { version: 1, slide_id: "sky-1", time_ms: 100 };
    const first = savePosition(owner, "sky", p);
    const second = savePosition(owner, "sky", { ...p, time_ms: 200 });
    await waitFor(() => expect(times).toEqual([100]));
    release();
    await first;
    await second;
    expect(times).toEqual([100, 200]);
    vi.mocked(session.request).mockResolvedValueOnce(null);
    await expect(savePosition(owner, "sky", p)).rejects.toThrow(
      "position_unavailable",
    );
    current.mockReturnValue(null);
    session.clear();
    await expect(savePosition(owner, "sky", p)).rejects.toThrow(
      "session_changed",
    );
    expect(readLocalPosition(owner, playerManifest())).toBeNull();
  });
  it("emits viewed only for active ended, deduplicates it, and ignores replaced iframe events", async () => {
    vi.useFakeTimers({
      toFake: ["performance", "setInterval", "clearInterval"],
    });
    const owner = "a1111111-1111-4111-8111-111111111111";
    vi.spyOn(session, "current").mockReturnValue({
      id: owner,
      display_name: "Private learner",
      locale: "en",
      time_zone: "UTC",
      background: "",
      analytics_opt_out: false,
    });
    vi.spyOn(session, "request").mockImplementation(
      async (path, method, body) => {
        if (path.endsWith("/manifest")) return playerManifest() as never;
        if (path.includes("/slides/"))
          return { html: "<p>Private slide text</p>" } as never;
        return (method === "PUT" ? body : null) as never;
      },
    );
    configureAnalytics({ learnerId: owner, optedOut: false, platform: "web" });
    const router = createRouter({
      history: createMemoryHistory({ initialEntries: ["/"] }),
      routeTree: createRootRoute({ component: () => <LessonPlayer /> }),
    });
    const mounted = render(
      <QueryClientProvider client={new QueryClient()}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
    const oldFrame = (await screen.findByTitle("Slide 1")) as HTMLIFrameElement;
    fireEvent.load(oldFrame);
    const oldWindow = oldFrame.contentWindow;
    vi.spyOn(navigator, "onLine", "get").mockReturnValue(false);
    const viewed = () =>
      vi
        .mocked(posthog.capture)
        .mock.calls.filter((call) => call[0] === "slide_viewed");
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(viewed()).toHaveLength(0);
    const ended = (source: Window | null) =>
      window.dispatchEvent(
        new MessageEvent("message", {
          source,
          origin: "null",
          data: { type: "ended" },
        }),
      );
    act(() => {
      ended(window);
    });
    expect(viewed()).toHaveLength(0);
    act(() => {
      ended(oldFrame.contentWindow);
      ended(oldFrame.contentWindow);
    });
    expect(viewed()).toHaveLength(1);
    expect(viewed()[0][1]).toMatchObject({
      watch_ms: 3000,
      offline: true,
      index: 0,
    });
    fireEvent.click(screen.getByRole("button", { name: "Replay slide" }));
    expect(
      vi
        .mocked(posthog.capture)
        .mock.calls.filter((c) => c[0] === "slide_replayed"),
    ).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Pause" }));
    expect(
      vi
        .mocked(posthog.capture)
        .mock.calls.filter((c) => c[0] === "slide_paused"),
    ).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    const nextFrame = (await screen.findByTitle(
      "Slide 2",
    )) as HTMLIFrameElement;
    fireEvent.load(nextFrame);
    act(() => {
      ended(oldWindow);
    });
    expect(viewed()).toHaveLength(1);
    await waitFor(() => expect(screen.getByText("Slide 2 of 3")).toBeVisible());
    expect(JSON.stringify(vi.mocked(posthog.capture).mock.calls)).not.toContain(
      "Private",
    );
    mounted.unmount();
  });
  it("retains pause/seek, measures wall watch time at speed and ends once per attempt", () => {
    const clock = new PlaybackClock(3000, 0);
    clock.play(100);
    clock.tick(600);
    expect(clock.snapshot().time).toBe(500);
    clock.pause(600);
    clock.tick(1600);
    expect(clock.snapshot().time).toBe(500);
    clock.seek(2000, 1600);
    expect(clock.snapshot().playing).toBe(false);
    clock.rate(2, 1600);
    clock.play(1600);
    clock.tick(2100);
    expect(clock.snapshot()).toMatchObject({
      time: 3000,
      playing: false,
      ended: true,
      watch: 1000,
    });
    expect(clock.end()).toBe(false);
    clock.replay(2200);
    expect(clock.snapshot()).toMatchObject({
      time: 0,
      playing: true,
      ended: false,
      watch: 0,
    });
    clock.seek(NaN, 2200);
    clock.rate(Infinity, 2200);
    clock.tick(2300);
    expect(clock.snapshot().time).toBe(200);
  });
  it("rejects stale/foreign/malformed messages and unregistered checkpoints", () => {
    const active = {} as Window,
      stale = {} as Window;
    const message = (data: unknown, source = active) =>
      ({ source, origin: "null", data }) as MessageEvent;
    expect(
      isSlideMessage(message({ type: "checkpoint", name: "turn" }), active, [
        "turn",
      ]),
    ).toBe(true);
    for (const event of [
      message({ type: "ended" }, stale),
      message({ type: "ended", time: 3000 }),
      message({ type: "checkpoint", name: "private text" }),
      message({ type: "checkpoint", name: "unknown" }),
    ])
      expect(isSlideMessage(event, active, ["turn"])).toBe(false);
  });
  it("validates bounded cursors, scopes local state and clears it on logout", () => {
    const m = playerManifest();
    const position = { version: 1, slide_id: "sky-2", time_ms: 1800 };
    expect(validPosition(position, m)).toBe(true);
    for (const p of [
      { ...position, time_ms: -1 },
      { ...position, time_ms: 3001 },
      { ...position, time_ms: 1.5 },
      { ...position, version: 2 },
      { ...position, slide_id: "foreign" },
      { ...position, completed: true },
    ])
      expect(validPosition(p, m)).toBe(false);
    writeLocalPosition("owner-a", "sky", position);
    expect(readLocalPosition("owner-b", m)).toBeNull();
    expect(readLocalPosition("owner-a", m)).toEqual(position);
    session.clear();
    expect(readLocalPosition("owner-a", m)).toBeNull();
    expect(playerReturnPath("/courses", "astronomy")).toBe("/courses");
    for (const path of [
      "https://evil.test",
      "//evil.test",
      "/lesson/sky",
      "/courses?text=private",
      "/\\evil",
    ])
      expect(playerReturnPath(path, "astronomy")).toBe("/course/astronomy");
  });
  it("delivers closed control/view schemas with UUID/common properties and consent", () => {
    configureAnalytics({
      learnerId: "a1111111-1111-4111-8111-111111111111",
      optedOut: false,
      platform: "web",
    });
    for (const event of [
      "slide_viewed",
      "slide_replayed",
      "slide_paused",
    ] as const)
      trackPlayer(event, {
        lesson_id: "sky",
        slide_id: "sky-1",
        index: 0,
        watch_ms: 1000,
        offline: true,
        text: "private",
      } as never);
    expect(posthog.capture).toHaveBeenCalledTimes(3);
    expect(posthog.capture).toHaveBeenCalledWith(
      "slide_viewed",
      expect.objectContaining({
        distinct_id: "a1111111-1111-4111-8111-111111111111",
        source: "client",
        platform: "web",
        app_version: "0.1.0",
        index: 0,
        watch_ms: 1000,
        offline: true,
      }),
    );
    expect(JSON.stringify(vi.mocked(posthog.capture).mock.calls)).not.toContain(
      "private",
    );
    configureAnalytics({
      learnerId: "a1111111-1111-4111-8111-111111111111",
      optedOut: true,
      platform: "web",
    });
    trackPlayer("slide_viewed", {
      lesson_id: "sky",
      slide_id: "sky-1",
      index: 0,
      watch_ms: 0,
      offline: false,
    });
    expect(posthog.capture).toHaveBeenCalledTimes(3);
  });
});
