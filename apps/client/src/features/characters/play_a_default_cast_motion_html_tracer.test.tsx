import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  validateCharacterSpec,
  compileRigSvg,
  getDefaultCastFixtures,
  getBundledFallbackSpec,
  type CharacterSpec,
} from "./rig";
import {
  trackCharacterRendered,
  trackCharacterRenderFallback,
  resetCharacterRenderDeduplication,
} from "./analytics";
import posthog from "posthog-js/dist/module.no-external";
import { configureAnalytics } from "../../lib/analytics";

vi.mock("posthog-js/dist/module.no-external", () => ({
  default: {
    capture: vi.fn(),
    has_opted_out_capturing: vi.fn(() => false),
    opt_in_capturing: vi.fn(),
    opt_out_capturing: vi.fn(),
    identify: vi.fn(),
  },
}));

describe("E17-01 Client Character Rig & Tracer Unit Tests", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetCharacterRenderDeduplication();
    configureAnalytics({
      learnerId: "11111111-1111-4111-8111-111111111111",
      platform: "web",
      appVersion: "0.1.0",
      optedOut: false,
    });
  });

  describe("Closed schema and cross-field bounds", () => {
    it("validates the original provisional guide fixture", () => {
      const fixtures = getDefaultCastFixtures();
      expect(fixtures).toHaveLength(3);
      const guide = fixtures[0];
      const result = validateCharacterSpec(guide);
      expect(result.valid).toBe(true);
      expect(result.errors).toBeUndefined();
    });

    it("rejects unknown properties (closed schema)", () => {
      const fixtures = getDefaultCastFixtures();
      const invalid = {
        ...fixtures[0],
        unknown_field: "should_fail",
      };
      const result = validateCharacterSpec(invalid);
      expect(result.valid).toBe(false);
      expect(result.errors).toContain("unknown_field");
    });

    it("rejects executable markup and script injection in text fields", () => {
      const fixtures = getDefaultCastFixtures();
      const injected: CharacterSpec = {
        ...fixtures[0],
        info: {
          ...fixtures[0].info,
          name: "<script>alert('xss')</script>",
        },
      };
      const result = validateCharacterSpec(injected);
      expect(result.valid).toBe(false);
      expect(result.errors).toContain("markup_rejected");
    });

    it("rejects external URLs and provider IDs in character data", () => {
      const fixtures = getDefaultCastFixtures();
      const urlInjected: CharacterSpec = {
        ...fixtures[0],
        voice_key: "elevenlabs:21m00Tcm4TlvDq8ikWAM",
      };
      const result = validateCharacterSpec(urlInjected);
      expect(result.valid).toBe(false);
    });

    it("rejects low contrast palette combinations", () => {
      const fixtures = getDefaultCastFixtures();
      const lowContrast: CharacterSpec = {
        ...fixtures[0],
        appearance: {
          ...fixtures[0].appearance,
          palette: {
            body: "#222222",
            accent: "#222222",
            outline: "#222222",
          },
        },
      };
      const result = validateCharacterSpec(lowContrast);
      expect(result.valid).toBe(false);
      expect(result.errors).toContain("invalid_contrast");
    });

    it("rejects incompatible accessories (> 2 or duplicates)", () => {
      const fixtures = getDefaultCastFixtures();
      const tooManyAccessories = {
        ...fixtures[0],
        appearance: {
          ...fixtures[0].appearance,
          accessories: ["glasses", "scarf", "hat"] as unknown as (
            "glasses" | "hat" | "scarf" | "badge"
          )[],
        },
      };
      const result = validateCharacterSpec(tooManyAccessories);
      expect(result.valid).toBe(false);
    });
  });

  describe("Trusted rig compilation and expression identity", () => {
    it("compiles parametric SVG for all 5 expressions preserving invariant base shape", () => {
      const guide = getDefaultCastFixtures()[0];
      const expressions = [
        "idle",
        "happy",
        "sad",
        "thinking",
        "celebrate",
      ] as const;

      const compiled = expressions.map((exp) => ({
        exp,
        svg: compileRigSvg(guide, exp),
      }));

      // Base body color and outline must be in every SVG
      for (const { svg } of compiled) {
        expect(svg).toContain(guide.appearance.palette.body);
        expect(svg).toContain(guide.appearance.palette.outline);
        expect(svg).toContain('data-part="body-shape"');
        expect(svg).not.toContain("<script");
        expect(svg).not.toContain("javascript:");
      }

      // Expressions must produce different facial feature outputs
      const idle = compiled.find((c) => c.exp === "idle")!.svg;
      const happy = compiled.find((c) => c.exp === "happy")!.svg;
      const sad = compiled.find((c) => c.exp === "sad")!.svg;
      expect(happy).not.toEqual(idle);
      expect(sad).not.toEqual(idle);
    });

    it("compiles safe fallback on missing rig or invalid spec without throwing", () => {
      const fallbackSpec = getBundledFallbackSpec();
      const svg = compileRigSvg(fallbackSpec, "idle");
      expect(svg).toContain("fallback-mascot");
      expect(svg).toContain("<svg");
    });
  });

  describe("Privacy-safe deduplicated analytics events", () => {
    it("emits character_rendered once per slide speaker per visit with no private content", () => {
      trackCharacterRendered({
        speaker_key: "guide",
        character_version: 1,
        rig_version: "original-v1",
        expression: "idle",
        offline: false,
        slide_id: "slide-1",
      });

      // Duplicate render on same slide visit should be ignored
      trackCharacterRendered({
        speaker_key: "guide",
        character_version: 1,
        rig_version: "original-v1",
        expression: "happy",
        offline: false,
        slide_id: "slide-1",
      });

      expect(posthog.capture).toHaveBeenCalledTimes(1);
      const call = vi.mocked(posthog.capture).mock.calls[0];
      expect(call[0]).toBe("character_rendered");
      const props = call[1] as Record<string, unknown>;
      expect(props.speaker_key).toBe("guide");
      expect(props.rig_version).toBe("original-v1");
      expect(props.character_version).toBe(1);
      // Privacy invariant: no character text, persona, bio, or SVG
      expect(props.name).toBeUndefined();
      expect(props.bio).toBeUndefined();
      expect(props.persona).toBeUndefined();
      expect(props.svg).toBeUndefined();
      expect(props.dialogue).toBeUndefined();
    });

    it("emits character_render_fallback with reason code on missing rig", () => {
      trackCharacterRenderFallback({
        reason_code: "missing_rig",
        offline: true,
      });

      expect(posthog.capture).toHaveBeenCalledWith(
        "character_render_fallback",
        expect.objectContaining({
          reason_code: "missing_rig",
          offline: true,
          source: "client",
        }),
      );
    });
  });
});
