import { describe, expect, it, vi, beforeEach } from "vitest";
import posthog from "posthog-js/dist/module.no-external";
import { isManifest, sandboxDocument } from "./manifest";
import { isSlideMessage } from "../lesson-player/bridge";
import { configureAnalytics, trackPlayer } from "../../lib/analytics";
vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
  },
}));
const manifest = {
  lesson_id: "lesson-1",
  course_id: "course-1",
  title: "Private slide title",
  version: 1,
  asset_origin: "https://assets.example.test",
  slides: [
    {
      slide_id: "sky-1",
      html_sha256:
        "a58876eb52bda6f6cca061b179aa66c358d8276647ffaba91b2201ae480eb9c2",
      asset_sha256: {},
      duration_ms: 3000,
      html_url: "/v1/lessons/lesson-1/slides/sky-1",
      assets: [],
      checkpoints: [],
      narration_status: "disabled",
      narration_text: "Private narration",
    },
  ],
};
describe("authorized lesson boundary", () => {
  beforeEach(() => vi.clearAllMocks());
  it("rejects missing, duplicate, foreign asset, oversized and assessment-bearing manifests", () => {
    expect(isManifest(manifest)).toBe(true);
    for (const value of [
      null,
      {},
      { ...manifest, slides: [] },
      { ...manifest, slides: [manifest.slides[0], manifest.slides[0]] },
      { ...manifest, answer_key: "private" },
      { ...manifest, asset_origin: "https://evil.test/path" },
      {
        ...manifest,
        slides: [
          { ...manifest.slides[0], html_url: "https://evil.test/slide" },
        ],
      },
      { ...manifest, slides: [{ ...manifest.slides[0], duration_ms: -1 }] },
      {
        ...manifest,
        slides: [
          { ...manifest.slides[0], assets: ["https://evil.test/track"] },
        ],
      },
    ])
      expect(isManifest(value)).toBe(false);
  });
  it("installs a CSP before untrusted markup with only one asset origin", () => {
    const html = sandboxDocument(
      '<html><head></head><body><script>fetch("https://evil.test")</script></body></html>',
      manifest.asset_origin,
      true,
    );
    expect(html.indexOf("Content-Security-Policy")).toBeLessThan(
      html.indexOf("fetch("),
    );
    expect(html).toContain("default-src 'none'");
    expect(html).toContain("connect-src https://assets.example.test;");
    expect(html).toContain("base-uri 'none'");
    expect(html).toContain('data-motion="reduce"');
    expect(() =>
      sandboxDocument("<p>Hi</p>", "https://evil.test/' unsafe", false),
    ).toThrow();
  });
  it("rejects extra keys, wrong frame/origin, unknown checkpoint and malformed messages", () => {
    const source = {} as Window;
    const event = (data: unknown, origin = "null", from: unknown = source) =>
      ({ source: from, origin, data }) as MessageEvent;
    expect(isSlideMessage(event({ type: "ended" }), source, [])).toBe(true);
    for (const e of [
      event({ type: "ended", complete: true }),
      event({ type: "checkpoint", name: "private text" }),
      event({ type: "ended" }, "https://evil.test"),
      event({ type: "ended" }, "null", {}),
      event([]),
      event(null),
    ])
      expect(isSlideMessage(e, source, [])).toBe(false);
  });
  it("projects only registered IDs/codes and honors UUID identity and opt-out", () => {
    configureAnalytics({
      learnerId: "a1111111-1111-4111-8111-111111111111",
      optedOut: false,
      platform: "web",
    });
    trackPlayer("lesson_started", {
      lesson_id: "lesson-1",
      course_id: "course-1",
      text: "private",
    } as never);
    expect(posthog.capture).toHaveBeenCalledWith(
      "lesson_started",
      expect.objectContaining({
        lesson_id: "lesson-1",
        course_id: "course-1",
        source: "client",
        platform: "web",
        app_version: "0.1.0",
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
    trackPlayer("lesson_player_error", {
      lesson_id: "lesson-1",
      error_code: "manifest_unavailable",
    });
    expect(posthog.capture).toHaveBeenCalledTimes(1);
    configureAnalytics(null);
  });
});

it("blocks navigation, forms, bases and network access for dynamic documents", () => {
  const html = sandboxDocument(
    '<a target="_top" href="https://evil.test">go</a>',
    "",
    false,
  );
  for (const directive of [
    "default-src 'none'",
    "connect-src 'none'",
    "base-uri 'none'",
    "form-action 'none'",
    "navigate-to 'none'",
  ])
    expect(html).toContain(directive);
  expect(html).not.toContain(location.origin);
});
