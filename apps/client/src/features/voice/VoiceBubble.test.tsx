import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ pricing: vi.fn(), transcribe: vi.fn() }));
vi.mock("../../lib/api", () => ({
  api: { pricing: mocks.pricing },
  readPricingCache: () => undefined,
}));
vi.mock("../../lib/analytics", () => ({ trackVoice: vi.fn() }));
vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  transcribe: mocks.transcribe,
}));
import { ToastHost, resetToasts } from "../../components/toast";
import { VoiceButton } from "./VoiceButton";
import type { VoicePlatform } from "./platform";

const LESSON = "11111111-1111-4111-8111-111111111111";
const platform: VoicePlatform = {
  supported: () => true,
  start: async () => ({
    stop: async () => new Blob([new Uint8Array(3000)]),
    cancel: () => {},
    level: () => 0.5,
    peak: () => 0.5,
  }),
};
function view(props: { disabled?: boolean; disabledReason?: string } = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <VoiceButton
        purpose="tutor"
        entryPoint="answer_panel"
        lessonId={LESSON}
        variant="bubble"
        onTranscript={vi.fn()}
        platform={platform}
        {...props}
      />
      <ToastHost />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  localStorage.clear();
  localStorage.setItem("duolify.voice.explained", "1");
  mocks.pricing.mockResolvedValue({ transcribe: { daily_requests: 60 } });
});
afterEach(() => {
  cleanup();
  act(() => resetToasts());
});

describe("VoiceBubble", () => {
  it("is an icon-only round control with no checkbox or helper text", async () => {
    view();
    const mic = await screen.findByTestId("voice-mic");
    expect(mic.textContent).toBe("");
    expect(mic).toHaveAccessibleName("Hold to talk");
    expect(screen.queryByRole("checkbox")).toBeNull();
  });

  it("shows the recording state on the bubble: timer badge and level meter", async () => {
    view();
    const mic = await screen.findByTestId("voice-mic");
    await waitFor(() => expect(mic).toHaveAttribute("aria-disabled", "false"));
    await act(async () => void fireEvent.pointerDown(mic, { pointerId: 1 }));
    expect(await screen.findByTestId("voice-timer")).toBeInTheDocument();
    expect(
      screen.getByRole("meter", { name: "Microphone level" }),
    ).toBeInTheDocument();
    await act(async () => void fireEvent.pointerCancel(mic, { pointerId: 1 }));
  });

  it("a disabled bubble keeps the reason in its description and toasts it on tap", async () => {
    view({ disabled: true, disabledReason: "Wait for the answer to finish." });
    const mic = await screen.findByTestId("voice-mic");
    expect(mic).toHaveAttribute("aria-disabled", "true");
    expect(mic).toHaveAccessibleDescription("Wait for the answer to finish.");
    await act(async () => void fireEvent.pointerDown(mic, { pointerId: 1 }));
    expect(await screen.findByTestId("toast")).toHaveTextContent(
      "Wait for the answer to finish.",
    );
  });
});
