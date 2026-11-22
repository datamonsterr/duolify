import { describe, expect, it, vi } from "vitest";
import {
  afterFirstPaint,
  createLazyPosthog,
  type PosthogLike,
} from "./lazyPosthog";

function harness(enabled = true) {
  const sdk = {
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    identify: vi.fn(),
    capture: vi.fn(),
  } satisfies PosthogLike;
  const calls: string[] = [];
  let paint: (() => void) | undefined;
  let resolveLoad!: (sdk: PosthogLike) => void;
  const load = vi.fn(
    () =>
      new Promise<PosthogLike>((resolve) => {
        calls.push("load");
        resolveLoad = resolve;
      }),
  );
  const init = vi.fn(() => calls.push("init"));
  const ph = createLazyPosthog({
    enabled,
    load,
    init,
    schedule: (run) => (paint = run),
  });
  const firstPaint = () => paint?.();
  const arrive = async () => {
    resolveLoad(sdk);
    await Promise.resolve();
    await Promise.resolve();
  };
  return { sdk, ph, load, init, calls, firstPaint, arrive };
}

describe("lazy posthog", () => {
  it("does not load the SDK before consent is resolved or before first paint", () => {
    const h = harness();
    h.ph.capture("early", { a: 1 });
    h.firstPaint();
    expect(h.load).not.toHaveBeenCalled();
    h.ph.opt_in_capturing();
    expect(h.load).not.toHaveBeenCalled();
    h.firstPaint();
    expect(h.load).toHaveBeenCalledOnce();
  });

  it("delivers events captured before the SDK loaded, in order, after init", async () => {
    const h = harness();
    h.ph.opt_in_capturing();
    h.ph.identify("learner");
    h.ph.capture("first", { n: 1 });
    h.firstPaint();
    h.ph.capture("second", { n: 2 });
    expect(h.sdk.capture).not.toHaveBeenCalled();
    await h.arrive();
    expect(h.calls).toEqual(["load", "init"]);
    expect(h.sdk.opt_in_capturing).toHaveBeenCalledOnce();
    expect(h.sdk.identify).toHaveBeenCalledWith("learner");
    expect(h.sdk.capture.mock.calls).toEqual([
      ["first", { n: 1 }],
      ["second", { n: 2 }],
    ]);
    h.ph.capture("third");
    expect(h.sdk.capture).toHaveBeenLastCalledWith("third", undefined);
  });

  it("sends nothing and never loads when the learner opts out", () => {
    const h = harness();
    h.ph.capture("before", {});
    h.ph.opt_out_capturing();
    h.ph.capture("after", {});
    h.firstPaint();
    expect(h.ph.has_opted_out_capturing()).toBe(true);
    expect(h.load).not.toHaveBeenCalled();
    expect(h.sdk.capture).not.toHaveBeenCalled();
  });

  it("drops the queue on opt-out and cancels a scheduled load", () => {
    const h = harness();
    h.ph.opt_in_capturing();
    h.ph.capture("queued", {});
    h.ph.opt_out_capturing();
    h.firstPaint();
    expect(h.load).not.toHaveBeenCalled();
    expect(h.sdk.capture).not.toHaveBeenCalled();
  });

  it("does not initialise an SDK that arrives after an opt-out", async () => {
    const h = harness();
    h.ph.opt_in_capturing();
    h.ph.capture("queued", {});
    h.firstPaint();
    h.ph.opt_out_capturing();
    await h.arrive();
    expect(h.init).not.toHaveBeenCalled();
    expect(h.sdk.capture).not.toHaveBeenCalled();
  });

  it("forwards consent changes straight to a loaded SDK", async () => {
    const h = harness();
    h.ph.opt_in_capturing();
    h.firstPaint();
    await h.arrive();
    h.ph.opt_out_capturing();
    expect(h.sdk.opt_out_capturing).toHaveBeenCalledOnce();
    h.sdk.has_opted_out_capturing.mockReturnValue(true);
    expect(h.ph.has_opted_out_capturing()).toBe(true);
  });

  it("queues and loads nothing without a project key", () => {
    const h = harness(false);
    h.ph.opt_in_capturing();
    h.ph.capture("x", {});
    h.firstPaint();
    expect(h.load).not.toHaveBeenCalled();
  });

  it("drops the queue when the SDK fails to load", async () => {
    const sdk = harness().sdk;
    const ph = createLazyPosthog({
      enabled: true,
      load: () => Promise.reject(new Error("offline")),
      schedule: (run) => run(),
    });
    ph.opt_in_capturing();
    await Promise.resolve();
    await Promise.resolve();
    expect(() => ph.capture("x", {})).not.toThrow();
    expect(sdk.capture).not.toHaveBeenCalled();
  });
});

describe("afterFirstPaint", () => {
  it("runs once, after two frames and idle time", async () => {
    vi.useFakeTimers();
    const frames: (() => void)[] = [];
    vi.stubGlobal("requestAnimationFrame", (cb: () => void) => frames.push(cb));
    const idle: (() => void)[] = [];
    vi.stubGlobal("requestIdleCallback", (cb: () => void) => idle.push(cb));
    const run = vi.fn();
    afterFirstPaint(run);
    expect(run).not.toHaveBeenCalled();
    frames.shift()!();
    frames.shift()!();
    expect(run).not.toHaveBeenCalled();
    idle.shift()!();
    expect(run).toHaveBeenCalledOnce();
    vi.advanceTimersByTime(6000);
    expect(run).toHaveBeenCalledOnce();
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("still runs in a background tab where frames never fire", () => {
    vi.useFakeTimers();
    vi.stubGlobal("requestAnimationFrame", () => 0);
    const run = vi.fn();
    afterFirstPaint(run);
    vi.advanceTimersByTime(5000);
    expect(run).toHaveBeenCalledOnce();
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });
});
