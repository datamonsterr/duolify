import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  pricing: vi.fn(),
  transcribe: vi.fn(),
  track: vi.fn(),
}));
vi.mock("../../lib/api", () => ({
  api: { pricing: mocks.pricing },
  readPricingCache: () => undefined,
}));
vi.mock("../../lib/analytics", () => ({ trackVoice: mocks.track }));
vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  transcribe: mocks.transcribe,
}));
import { VoiceButton } from "./VoiceButton";
import { resetToasts } from "../../components/toast";
import type { VoicePlatform } from "./platform";

const LESSON = "11111111-1111-4111-8111-111111111111";
const platform = (deny = false): VoicePlatform => ({
  supported: () => true,
  start: async () => {
    if (deny) throw Object.assign(new Error("x"), { name: "NotAllowedError" });
    return {
      stop: async () => new Blob([new Uint8Array(3000)]),
      cancel: () => {},
      level: () => 0.5,
      peak: () => 0.5,
    };
  },
});
function view(
  p: VoicePlatform,
  onTranscript = vi.fn(),
  lessonId: string | null = LESSON,
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <VoiceButton
        purpose="tutor"
        entryPoint="answer_panel"
        lessonId={lessonId ?? undefined}
        onTranscript={onTranscript}
        platform={p}
      />
    </QueryClientProvider>,
  );
  return onTranscript;
}
const setOnline = (v: boolean) =>
  Object.defineProperty(navigator, "onLine", { value: v, configurable: true });

beforeEach(() => {
  resetToasts();
  localStorage.clear();
  localStorage.setItem("duolify.voice.explained", "1");
  setOnline(true);
  mocks.pricing.mockResolvedValue({ transcribe: { daily_requests: 60 } });
  mocks.transcribe.mockResolvedValue({
    text: "hello tutor",
    language: "en",
    duration_ms: 3000,
    served_model: "m/x",
    fallback_used: false,
  });
  mocks.track.mockClear();
});

describe("VoiceButton", () => {
  it("hold shows the recording indicator, timer and level meter; release delivers the transcript", async () => {
    const onTranscript = view(platform());
    const mic = await screen.findByTestId("voice-mic");
    await waitFor(() => expect(mic).toHaveAttribute("aria-disabled", "false"));
    await act(async () => void fireEvent.pointerDown(mic, { pointerId: 1 }));
    expect(await screen.findByTestId("voice-live")).toBeInTheDocument();
    expect(screen.getByTestId("voice-timer")).toHaveTextContent("0:0");
    expect(
      screen.getByRole("meter", { name: "Microphone level" }),
    ).toBeInTheDocument();
    expect(mic).toHaveAttribute(
      "aria-label",
      expect.stringContaining("Release to send"),
    );
    await act(async () => new Promise((r) => setTimeout(r, 620)));
    await act(async () => void fireEvent.pointerUp(mic, { pointerId: 1 }));
    await waitFor(() =>
      expect(onTranscript).toHaveBeenCalledWith({
        text: "hello tutor",
        language: "en",
      }),
    );
  });

  it("sliding the pointer off the control cancels without uploading", async () => {
    view(platform());
    const mic = await screen.findByTestId("voice-mic");
    await waitFor(() => expect(mic).toHaveAttribute("aria-disabled", "false"));
    mic.getBoundingClientRect = () => ({
      left: 0,
      right: 40,
      top: 0,
      bottom: 40,
      width: 40,
      height: 40,
      x: 0,
      y: 0,
      toJSON() {},
    });
    await act(async () => void fireEvent.pointerDown(mic, { pointerId: 1 }));
    await act(async () => new Promise((r) => setTimeout(r, 620)));
    await act(
      async () =>
        void fireEvent.pointerMove(mic, {
          pointerId: 1,
          clientX: 300,
          clientY: 300,
        }),
    );
    await waitFor(() => expect(screen.queryByTestId("voice-live")).toBeNull());
    expect(mocks.transcribe).not.toHaveBeenCalled();
    expect(mocks.track.mock.calls.map((c) => c[0].event)).toContain(
      "voice_recording_cancelled",
    );
  });

  it("first use explains why the microphone is needed before asking for permission", async () => {
    localStorage.clear();
    view(platform());
    const mic = await screen.findByTestId("voice-mic");
    await waitFor(() => expect(mic).toHaveAttribute("aria-disabled", "false"));
    fireEvent.pointerDown(mic, { pointerId: 1 });
    const explain = await screen.findByTestId("voice-explain");
    expect(explain).toHaveTextContent("does not keep it (zero data retention)");
    expect(explain.textContent).not.toMatch(/never stored/i);
    fireEvent.click(screen.getByTestId("voice-explain-allow"));
    await waitFor(() =>
      expect(screen.queryByTestId("voice-explain")).toBeNull(),
    );
    expect(localStorage.getItem("duolify.voice.explained")).toBe("1");
  });

  it("explains a denied permission and keeps typing available", async () => {
    view(platform(true));
    const mic = await screen.findByTestId("voice-mic");
    await waitFor(() => expect(mic).toHaveAttribute("aria-disabled", "false"));
    await act(async () => void fireEvent.pointerDown(mic, { pointerId: 1 }));
    expect(await screen.findByTestId("toast")).toHaveTextContent(
      "Microphone access is off",
    );
    expect(screen.getByTestId("toast-action")).toHaveTextContent("Try again");
  });

  it("says voice needs a connection when offline", async () => {
    setOnline(false);
    view(platform());
    expect(await screen.findByTestId("toast")).toHaveTextContent(
      "needs a connection",
    );
    expect(screen.getByTestId("voice-mic")).toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  it("is unavailable without a transcription rate", async () => {
    mocks.pricing.mockResolvedValue({ transcribe: null });
    view(platform());
    expect(await screen.findByTestId("toast")).toHaveTextContent(
      "not available yet",
    );
  });

  it("names when the daily cap resets", async () => {
    localStorage.setItem(
      "duolify.voice.cap-resets-at",
      new Date(Date.now() + 3_600_000).toISOString(),
    );
    view(platform());
    await waitFor(() =>
      expect(screen.getByTestId("toast")).toHaveTextContent(
        "today's voice limit",
      ),
    );
    expect(screen.getByTestId("toast")).toHaveTextContent("resets at");
  });

  it("renders nothing without a Lesson", () => {
    view(platform(), vi.fn(), null);
    expect(screen.queryByTestId("voice-ptt")).toBeNull();
  });

  it("offers retry after an empty transcript and creates nothing", async () => {
    mocks.transcribe.mockResolvedValueOnce({
      text: "",
      language: "",
      duration_ms: 3000,
      served_model: "m/x",
      fallback_used: false,
    });
    const onTranscript = view(platform());
    const mic = await screen.findByTestId("voice-mic");
    await waitFor(() => expect(mic).toHaveAttribute("aria-disabled", "false"));
    await act(async () => void fireEvent.pointerDown(mic, { pointerId: 1 }));
    await act(async () => new Promise((r) => setTimeout(r, 620)));
    await act(async () => void fireEvent.pointerUp(mic, { pointerId: 1 }));
    expect(await screen.findByTestId("toast")).toHaveTextContent(
      "could not make out",
    );
    expect(onTranscript).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("toast-action"));
    await waitFor(() => expect(onTranscript).toHaveBeenCalled());
  });

  it("leaving the button while a key is held cancels the take", async () => {
    const onTranscript = view(platform());
    const mic = await screen.findByTestId("voice-mic");
    await waitFor(() => expect(mic).toHaveAttribute("aria-disabled", "false"));
    await act(async () => void fireEvent.keyDown(mic, { key: " " }));
    await waitFor(() => expect(mic).toHaveAttribute("aria-pressed", "true"));
    await act(async () => void fireEvent.blur(mic));
    await waitFor(() => expect(mic).toHaveAttribute("aria-pressed", "false"));
    expect(onTranscript).not.toHaveBeenCalled();
  });
});
