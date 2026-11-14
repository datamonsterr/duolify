import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import type { ManifestSlide } from "../player/manifest";
import { NarrationControl } from "./NarrationControl";

const failed: ManifestSlide = {
  html_sha256: "a".repeat(64),
  asset_sha256: {},
  slide_id: "sky-1",
  duration_ms: 3000,
  html_url: "/v1/lessons/lesson-1/slides/sky-1",
  assets: [],
  checkpoints: [],
  narration_status: "failed",
  narration_text: "Stars are suns.",
};

describe("vendor outage falls back to captions only (S09 Narration unavailable)", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("explains captions-only, offers Retry and never offers Play", () => {
    render(
      <NarrationControl
        lessonId="lesson-1"
        slide={failed}
        rate={1}
        muted={false}
        captions
      />,
    );
    expect(screen.getByText("Narration unavailable")).toBeTruthy();
    expect(
      screen.getByText(
        "Read the captions and transcript, then continue. The animation still plays.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Retry narration" }),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Play narration" })).toBeNull();
  });
});
