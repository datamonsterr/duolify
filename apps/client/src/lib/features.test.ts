import { afterEach, describe, expect, it, vi } from "vitest";
import {
  fetchLessonTestTracerEnabled,
  isLessonTestTracerEnabled,
  setLessonTestTracerEnabledForTest,
} from "./features";
import { http, HttpResponse } from "msw";
import { server } from "../test/setup";

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

  it("resolves enabled from /v1/features with the VITE flag unset (web image)", async () => {
    vi.stubEnv("VITE_LESSON_TEST_TRACER_ENABLED", "");
    await expect(fetchLessonTestTracerEnabled()).resolves.toBe(true);
  });

  it("follows /v1/features when the server kill switch is off", async () => {
    vi.stubEnv("VITE_LESSON_TEST_TRACER_ENABLED", "");
    server.use(
      http.get("*/v1/features", () =>
        HttpResponse.json({ lesson_test_tracer_enabled: false }),
      ),
    );
    await expect(fetchLessonTestTracerEnabled()).resolves.toBe(false);
  });
});
