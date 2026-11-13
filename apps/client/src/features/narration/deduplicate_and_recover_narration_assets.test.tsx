import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { session } from "../accounts/session";
import type { ManifestSlide } from "../player/manifest";
import { NarrationControl } from "./NarrationControl";

const text = "Stars are suns.";
const slide = (over: Partial<ManifestSlide> = {}): ManifestSlide => ({
  html_sha256: "a".repeat(64),
  asset_sha256: {},
  slide_id: "sky-1",
  duration_ms: 3000,
  html_url: "/v1/lessons/lesson-1/slides/sky-1",
  assets: [],
  checkpoints: [],
  narration_status: "ready",
  narration: {
    audio_url: "/v1/lessons/lesson-1/slides/sky-1/narration/audio",
    alignment_url: "/v1/lessons/lesson-1/slides/sky-1/narration/alignment",
    duration_ms: text.length * 50,
    characters: text.length,
  },
  narration_text: text,
  ...over,
});
const view = (s: ManifestSlide, onRecovered = vi.fn()) =>
  render(
    <NarrationControl
      lessonId="lesson-1"
      slide={s}
      rate={1}
      muted={false}
      captions
      onRecovered={onRecovered}
    />,
  );

describe("narration recovery states (S09 Narration unavailable)", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "Audio",
      class {
        play = vi.fn(async () => undefined);
        pause = vi.fn();
        addEventListener = vi.fn();
      },
    );
    URL.createObjectURL = vi.fn(() => "blob:narration");
    URL.revokeObjectURL = vi.fn();
  });
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("offers Retry narration for failed audio and reports the new state", async () => {
    const request = vi
      .spyOn(session, "request")
      .mockResolvedValue({ state: "pending" });
    const onRecovered = vi.fn();
    view(
      slide({ narration_status: "failed", narration: undefined }),
      onRecovered,
    );
    expect(screen.getByText("Narration unavailable")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry narration" }));
    await waitFor(() => expect(onRecovered).toHaveBeenCalledTimes(1));
    expect(request).toHaveBeenCalledWith(
      "/v1/lessons/lesson-1/slides/sky-1/narration/retry",
      "POST",
    );
    expect(screen.getByText("Narration is being prepared.")).toBeTruthy();
  });

  it("keeps the retry action when the request fails", async () => {
    vi.spyOn(session, "request").mockRejectedValue(new Error("offline"));
    view(slide({ narration_status: "failed", narration: undefined }));
    fireEvent.click(screen.getByRole("button", { name: "Retry narration" }));
    expect(
      await screen.findByText(
        "Could not reach the server. Your place is saved; try again.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Retry narration" }),
    ).toBeTruthy();
  });

  it("restores a missing object, then waits for an explicit Play", async () => {
    const request = vi
      .spyOn(session, "request")
      .mockRejectedValueOnce(new Error("503"))
      .mockResolvedValueOnce({ state: "restored" });
    vi.spyOn(session, "blob").mockRejectedValue(new Error("503"));
    const onRecovered = vi.fn();
    view(slide(), onRecovered);
    fireEvent.click(screen.getByRole("button", { name: "Play narration" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Restore narration" }),
    );
    expect(
      await screen.findByText("Narration restored. Press Play when you are."),
    ).toBeTruthy();
    expect(request).toHaveBeenLastCalledWith(
      "/v1/lessons/lesson-1/slides/sky-1/narration/recover",
      "POST",
    );
    expect(onRecovered).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("button", { name: "Play narration" })).toBeTruthy();
  });

  it("explains operator regeneration while captions stay available", async () => {
    vi.spyOn(session, "request")
      .mockRejectedValueOnce(new Error("503"))
      .mockResolvedValueOnce({ state: "regenerating" });
    vi.spyOn(session, "blob").mockRejectedValue(new Error("503"));
    view(slide());
    fireEvent.click(screen.getByRole("button", { name: "Play narration" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Restore narration" }),
    );
    expect(
      await screen.findByText(
        "Narration is being restored. Captions stay available.",
      ),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Restore narration" }),
    ).toBeNull();
  });

  it("never charges or sends text: recovery posts no body", async () => {
    const request = vi
      .spyOn(session, "request")
      .mockResolvedValue({ state: "pending" });
    view(slide({ narration_status: "failed", narration: undefined }));
    fireEvent.click(screen.getByRole("button", { name: "Retry narration" }));
    await waitFor(() => expect(request).toHaveBeenCalled());
    expect(JSON.stringify(request.mock.calls)).not.toContain(text);
    expect(request.mock.calls[0]).toHaveLength(2);
  });
});
