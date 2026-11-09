import { useEffect, useState } from "react";

let overrideFlag: boolean | null = null;

export function setLessonTestTracerEnabledForTest(
  enabled: boolean | null,
): void {
  overrideFlag = enabled;
}

export function isLessonTestTracerEnabled(): boolean {
  if (overrideFlag !== null) {
    return overrideFlag;
  }
  const envVal = import.meta.env.VITE_LESSON_TEST_TRACER_ENABLED;
  if (envVal === "true") return true;
  if (envVal === "false") return false;
  if (
    typeof window !== "undefined" &&
    (window as unknown as { __LESSON_TEST_TRACER_ENABLED__?: boolean })
      .__LESSON_TEST_TRACER_ENABLED__ !== undefined
  ) {
    return Boolean(
      (window as unknown as { __LESSON_TEST_TRACER_ENABLED__?: boolean })
        .__LESSON_TEST_TRACER_ENABLED__,
    );
  }
  return false;
}

export async function fetchLessonTestTracerEnabled(): Promise<boolean> {
  if (overrideFlag !== null) return overrideFlag;
  try {
    const base = import.meta.env.VITE_API_URL || "http://localhost:8080";
    const res = await fetch(`${base}/v1/features`);
    if (res.ok) {
      const data = (await res.json()) as {
        lesson_test_tracer_enabled?: boolean;
      };
      if (typeof data.lesson_test_tracer_enabled === "boolean") {
        if (typeof window !== "undefined") {
          (
            window as unknown as { __LESSON_TEST_TRACER_ENABLED__?: boolean }
          ).__LESSON_TEST_TRACER_ENABLED__ = data.lesson_test_tracer_enabled;
        }
        return data.lesson_test_tracer_enabled;
      }
    }
  } catch {
    // network fallback
  }
  return isLessonTestTracerEnabled();
}

export function useLessonTestTracerEnabled(): boolean {
  const [enabled, setEnabled] = useState(isLessonTestTracerEnabled());

  useEffect(() => {
    let active = true;
    fetchLessonTestTracerEnabled().then((val) => {
      if (active) setEnabled(val);
    });
    return () => {
      active = false;
    };
  }, []);

  return enabled;
}
