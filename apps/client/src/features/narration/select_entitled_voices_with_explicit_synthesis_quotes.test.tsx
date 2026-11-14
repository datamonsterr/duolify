import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import posthog from "posthog-js/dist/module.no-external";
import { configureAnalytics } from "../../lib/analytics";
import { AuthError, session } from "../accounts/session";
import type { ManifestSlide } from "../player/manifest";
import { NarrationControl } from "./NarrationControl";
import { VoicePanel } from "./VoicePanel";
import type { VoiceList, VoiceQuote } from "./voices";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
  },
}));
vi.mock("../pricing/query", () => ({
  pricingQueryOptions: () => ({
    queryKey: ["pricing"],
    queryFn: async () => ({ version: "pricing-test" }),
    retry: false,
  }),
}));

const learnerId = "a1111111-1111-4111-8111-111111111111";
const list = (over: Partial<VoiceList> = {}): VoiceList => ({
  narration_enabled: true,
  selected: "default",
  entitled: true,
  upsell_allowed: false,
  config_hash: "hash-1",
  voices: [
    { key: "default", label: "Default voice", curated: false, available: true },
    {
      key: "fixture-warm",
      label: "Warm voice",
      curated: true,
      available: true,
    },
  ],
  ...over,
});
const quote = (over: Partial<VoiceQuote> = {}): VoiceQuote => ({
  lesson_id: "lesson-1",
  voice_key: "fixture-warm",
  quote_id: "quote-1",
  characters: 120,
  credits: 5,
  free: false,
  config_hash: "hash-1",
  ...over,
});

function mount(onChanged = vi.fn()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <VoicePanel lessonId="lesson-1" onChanged={onChanged} />
    </QueryClientProvider>,
  );
  return onChanged;
}
const calls = (request: { mock: { calls: unknown[][] } }) =>
  request.mock.calls.map(
    (c) => `${(c[1] as string) ?? "GET"} ${c[0] as string}`,
  );

describe("voice selection with explicit synthesis quotes", () => {
  beforeEach(() => {
    vi.mocked(posthog.capture).mockClear();
    configureAnalytics({
      learnerId,
      platform: "web",
      optedOut: false,
      appVersion: "0.1.0",
    });
  });
  afterEach(() => {
    cleanup();
    configureAnalytics(null);
    vi.restoreAllMocks();
  });

  it("shows the new character quote and declining changes and charges nothing", async () => {
    const request = vi
      .spyOn(session, "request")
      .mockImplementation(async (path: string) =>
        path.endsWith("/quote") ? quote() : list(),
      );
    mount();
    fireEvent.click(await screen.findByLabelText("Warm voice"));
    expect(await screen.findByText(/needs 120 new characters/)).toBeTruthy();
    expect(
      screen.getByText(/Nothing is charged until you confirm/),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Keep current voice" }));
    expect(
      await screen.findByText("Kept Default voice. Nothing was charged."),
    ).toBeTruthy();
    // Only the list and the quote were requested: no consent, no synthesis.
    expect(calls(request)).toEqual([
      "GET /v1/lessons/lesson-1/narration/voice",
      "POST /v1/lessons/lesson-1/narration/voice/quote",
    ]);
  });

  it("confirming sends the accepted quote and config hash, then follows the voice until ready", async () => {
    let state: VoiceList["change"];
    let applied = false;
    const request = vi
      .spyOn(session, "request")
      .mockImplementation(async (path: string, method?: string) => {
        if (path.endsWith("/quote")) return quote();
        if (method === "POST") {
          applied = true;
          state = { voice_key: "fixture-warm", state: "pending" };
          return { state: "queued", voice_key: "fixture-warm" };
        }
        return list({
          selected: applied ? "fixture-warm" : "default",
          change: state,
        });
      });
    const onChanged = mount();
    fireEvent.click(await screen.findByLabelText("Warm voice"));
    fireEvent.click(
      await screen.findByRole("button", { name: "Confirm and narrate" }),
    );
    expect(
      await screen.findByText(/Your current narration keeps playing/),
    ).toBeTruthy();
    expect(request).toHaveBeenCalledWith(
      "/v1/lessons/lesson-1/narration/voice",
      "POST",
      { voice_key: "fixture-warm", quote_id: "quote-1", config_hash: "hash-1" },
    );
    state = { voice_key: "fixture-warm", state: "ready" };
    expect(
      await screen.findByText(
        /Warm voice is ready. Press Play when you are./,
        {},
        { timeout: 4000 },
      ),
    ).toBeTruthy();
    expect(onChanged).toHaveBeenCalled();
  });

  it("a stored voice is a free switch with no quote credits", async () => {
    vi.spyOn(session, "request").mockImplementation(async (path: string) =>
      path.endsWith("/quote")
        ? quote({ free: true, characters: 0, credits: 0, quote_id: undefined })
        : list({ selected: "fixture-warm" }),
    );
    mount();
    fireEvent.click(await screen.findByLabelText("Default voice"));
    expect(await screen.findByText(/Switching is free/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Use this voice" })).toBeTruthy();
  });

  it("locks curated voices for a plan without the benefit and shows one upsell only when allowed", async () => {
    const locked = list({
      entitled: false,
      upsell_allowed: true,
      voices: [
        {
          key: "default",
          label: "Default voice",
          curated: false,
          available: true,
        },
        {
          key: "fixture-warm",
          label: "Warm voice",
          curated: true,
          available: false,
        },
      ],
    });
    vi.spyOn(session, "request").mockResolvedValue(locked);
    mount();
    const radio = (await screen.findByLabelText(
      /Warm voice/,
    )) as HTMLInputElement;
    expect(radio.disabled).toBe(true);
    expect(
      screen.getByText(/Curated voices come with Plus and Pro/),
    ).toBeTruthy();
    await waitFor(() => expect(posthog.capture).toHaveBeenCalledTimes(1));
    expect(posthog.capture).toHaveBeenCalledWith(
      "credits_upsell_shown",
      expect.objectContaining({
        distinct_id: learnerId,
        feature: "narration_voice",
        price_table_version: "pricing-test",
        source: "client",
      }),
    );
    cleanup();
    vi.mocked(posthog.capture).mockClear();
    // Learn/Plus/Pro include "No upsell banners": the API withholds the guidance.
    vi.spyOn(session, "request").mockResolvedValue({
      ...locked,
      upsell_allowed: false,
    });
    mount();
    await screen.findByLabelText(/Warm voice/);
    expect(screen.queryByText(/Curated voices come with/)).toBeNull();
    expect(posthog.capture).not.toHaveBeenCalled();
  });

  it("narration off explains itself and offers no voice choice", async () => {
    vi.spyOn(session, "request").mockResolvedValue(
      list({ narration_enabled: false }),
    );
    mount();
    expect(await screen.findByText(/Narration is off/)).toBeTruthy();
    expect(screen.queryByRole("radio")).toBeNull();
  });

  it("insufficient audio credits keep the current narration and offer recovery", async () => {
    vi.spyOn(session, "request").mockImplementation(
      async (path: string, method?: string) => {
        if (path.endsWith("/quote")) return quote();
        if (method === "POST")
          throw new AuthError("Conflict", 409, { code: "quote_changed" });
        return list();
      },
    );
    mount();
    fireEvent.click(await screen.findByLabelText("Warm voice"));
    fireEvent.click(
      await screen.findByRole("button", { name: "Confirm and narrate" }),
    );
    expect(await screen.findByText(/The quote changed/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Choose again" }));
    expect(screen.queryByText(/The quote changed/)).toBeNull();
  });

  it("carries no narration text or lesson text in the upsell event", async () => {
    vi.spyOn(session, "request").mockResolvedValue(
      list({ entitled: false, upsell_allowed: true }),
    );
    mount();
    await waitFor(() => expect(posthog.capture).toHaveBeenCalled());
    const payload = JSON.stringify(vi.mocked(posthog.capture).mock.calls);
    expect(payload).not.toContain("The sky");
  });
});

describe("audio paused for insufficient credits", () => {
  const paused: ManifestSlide = {
    html_sha256: "a".repeat(64),
    asset_sha256: {},
    slide_id: "sky-1",
    duration_ms: 3000,
    html_url: "/v1/lessons/lesson-1/slides/sky-1",
    assets: [],
    checkpoints: [],
    narration_status: "disabled",
    narration_reason: "insufficient_credits",
    narration_text: "The sky is a map.",
  };
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("disables only audio and offers Add credits and Retry", async () => {
    const request = vi
      .spyOn(session, "request")
      .mockResolvedValue({ state: "pending" });
    render(
      <NarrationControl
        lessonId="lesson-1"
        slide={paused}
        rate={1}
        muted={false}
        captions
      />,
    );
    expect(screen.getByText("Narration paused")).toBeTruthy();
    expect(
      screen.getByText(/The lesson text, captions and test still work/),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Add credits" }).getAttribute("href"),
    ).toBe("/credits");
    fireEvent.click(screen.getByRole("button", { name: "Retry narration" }));
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "/v1/lessons/lesson-1/slides/sky-1/narration/retry",
        "POST",
      ),
    );
    expect(
      await screen.findByText("Narration is being prepared."),
    ).toBeTruthy();
  });

  it("a disabled slide without a credit reason (narration off) shows no narration UI", () => {
    const { container } = render(
      <NarrationControl
        lessonId="lesson-1"
        slide={{ ...paused, narration_reason: undefined }}
        rate={1}
        muted={false}
        captions
      />,
    );
    expect(container.textContent).toBe("");
  });
});
