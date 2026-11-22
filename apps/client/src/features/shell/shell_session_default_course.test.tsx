import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { useShellSession } from "./ShellSession";

describe("shell active course", () => {
  beforeEach(() => localStorage.clear());

  it("has no active course for a learner who never chose one", () => {
    // A fixture id here made Learn render LearnPath("astronomy") -> "Could not load this course."
    const { result } = renderHook(() => useShellSession());
    expect(result.current.courseId()).toBe("");
  });

  it("restores the remembered course", () => {
    localStorage.setItem("duolify:active-course", "c-1");
    const { result } = renderHook(() => useShellSession());
    expect(result.current.courseId()).toBe("c-1");
  });
});
