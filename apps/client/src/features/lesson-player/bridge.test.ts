import { describe, expect, it } from "vitest";
import { isSlideMessage, nextSlide } from "./bridge";

describe("slide bridge", () => {
  it("accepts only messages from the sandboxed frame", () => {
    const source = {} as Window;
    expect(
      isSlideMessage(
        { origin: "null", source, data: { type: "ended" } } as MessageEvent,
        source,
      ),
    ).toBe(true);
    expect(
      isSlideMessage(
        {
          origin: "https://evil.test",
          source,
          data: { type: "ended" },
        } as MessageEvent,
        source,
      ),
    ).toBe(false);
    expect(
      isSlideMessage(
        { origin: "null", source: {}, data: { type: "ended" } } as MessageEvent,
        source,
      ),
    ).toBe(false);
  });
  it("stops at the final slide", () => {
    expect(nextSlide(0, 3)).toBe(1);
    expect(nextSlide(2, 3)).toBe(2);
  });
});

it("bounds telemetry strings and validates slide identifiers", () => {
  const source = {} as Window;
  const valid = {
    type: "character_rendered",
    speaker_key: "guide",
    character_version: 1,
    rig_version: "v1",
    expression: "neutral",
    offline: false,
    slide_id: "sky-1",
  };
  const accepts = (data: unknown) =>
    isSlideMessage({ origin: "null", source, data } as MessageEvent, source);
  expect(accepts(valid)).toBe(true);
  for (const slide_id of ["</script>", "has space", "x".repeat(101), 42])
    expect(accepts({ ...valid, slide_id })).toBe(false);
  for (const field of ["speaker_key", "rig_version", "expression"])
    expect(accepts({ ...valid, [field]: "x".repeat(101) })).toBe(false);
  expect(accepts({ ...valid, character_version: Infinity })).toBe(false);
  expect(
    accepts({
      type: "character_render_fallback",
      reason_code: "x".repeat(101),
      offline: false,
    }),
  ).toBe(false);
  expect(
    accepts({
      type: "player_keydown",
      key: "x".repeat(101),
      code: "ArrowRight",
    }),
  ).toBe(false);
});
