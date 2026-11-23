import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("./Onboarding", () => {
  throw new Error("Unable to preload CSS");
});

import { OnboardingGate } from "./OnboardingGate";

describe("OnboardingGate", () => {
  it("renders nothing when the onboarding chunk fails to load", async () => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const { container } = render(
      <>
        <p>Learn path</p>
        <OnboardingGate path="/" />
      </>,
    );
    expect(await screen.findByText("Learn path")).toBeTruthy();
    expect(container.querySelectorAll("p")).toHaveLength(1);
  });
});
