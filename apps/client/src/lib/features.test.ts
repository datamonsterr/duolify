import { afterEach, describe, expect, it, vi } from "vitest";
import {
  isLessonTestTracerEnabled,
  setLessonTestTracerEnabledForTest,
} from "./features";

describe("isLessonTestTracerEnabled (ADR 0017)", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    setLessonTestTracerEnabledForTest(null);
    delete (window as unknown as Record<string, unknown>)
      .__LESSON_TEST_TRACER_ENABLED__;
  });

  it("is enabled when nothing is configured", () => {
    vi.stubEnv("VITE_LESSON_TEST_TRACER_ENABLED", "");
    expect(isLessonTestTracerEnabled()).toBe(true);
  });

  it("is disabled by explicit VITE flag false", () => {
    vi.stubEnv("VITE_LESSON_TEST_TRACER_ENABLED", "false");
    expect(isLessonTestTracerEnabled()).toBe(false);
  });

  it("stays enabled for VITE flag true", () => {
    vi.stubEnv("VITE_LESSON_TEST_TRACER_ENABLED", "true");
    expect(isLessonTestTracerEnabled()).toBe(true);
  });

  it("honours the server-provided window flag set to false", () => {
    vi.stubEnv("VITE_LESSON_TEST_TRACER_ENABLED", "");
    (
      window as unknown as Record<string, unknown>
    ).__LESSON_TEST_TRACER_ENABLED__ = false;
    expect(isLessonTestTracerEnabled()).toBe(false);
  });
});
