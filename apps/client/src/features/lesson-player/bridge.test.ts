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
