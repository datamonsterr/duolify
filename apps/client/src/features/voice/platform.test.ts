import { afterEach, describe, expect, it, vi } from "vitest";
import { browserPlatform } from "./platform";

function install(state: "running" | "suspended" | "throws") {
  const track = { stop: vi.fn() };
  Object.defineProperty(navigator, "mediaDevices", {
    configurable: true,
    value: { getUserMedia: async () => ({ getTracks: () => [track] }) },
  });
  class Recorder {
    static isTypeSupported = () => true;
    state = "inactive";
    mimeType = "audio/webm";
    ondataavailable: unknown;
    onstop: (() => void) | null = null;
    start() {
      this.state = "recording";
    }
    stop() {
      this.state = "inactive";
      this.onstop?.();
    }
  }
  class Context {
    state = state === "running" ? "running" : "suspended";
    constructor() {
      if (state === "throws") throw new Error("no audio context");
    }
    resume = async () => {};
    close = async () => {};
    createAnalyser = () => ({
      fftSize: 0,
      getByteTimeDomainData: (a: Uint8Array) => a.fill(128),
    });
    createMediaStreamSource = () => ({ connect: () => {} });
  }
  vi.stubGlobal("MediaRecorder", Recorder);
  vi.stubGlobal("AudioContext", Context);
}
afterEach(() => vi.unstubAllGlobals());

describe("browser recorder level meter", () => {
  it("reports a quiet room as quiet when the meter runs", async () => {
    install("running");
    const handle = await browserPlatform.start();
    handle.level();
    expect(handle.peak()).toBe(0);
  });
  it.each(["throws", "suspended"] as const)(
    "does not call a take silent when the meter is %s",
    async (state) => {
      install(state);
      const handle = await browserPlatform.start();
      handle.level();
      expect(handle.peak()).toBeGreaterThan(0.02);
    },
  );
});
