import { act, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LESSON_FRAME_URL, SandboxFrame } from "./SandboxFrame";

function frameOf() {
  return screen.getByTitle("slide") as HTMLIFrameElement;
}
function say(source: Window | null, type: string) {
  act(() => {
    window.dispatchEvent(
      new MessageEvent("message", { source, data: { type } }),
    );
  });
}

describe("SandboxFrame", () => {
  it("loads the host page by URL in a sandbox without allow-same-origin, never by srcdoc", () => {
    render(<SandboxFrame title="slide" html="<p>one</p>" />);
    const frame = frameOf();
    expect(frame.getAttribute("src")).toBe(LESSON_FRAME_URL);
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame.hasAttribute("srcdoc")).toBe(false);
  });

  it("sends the document when the frame announces itself, and reports load once written", () => {
    const onLoad = vi.fn();
    render(<SandboxFrame title="slide" html="<p>one</p>" onLoad={onLoad} />);
    const target = frameOf().contentWindow!;
    const post = vi.spyOn(target, "postMessage");
    say(target, "frame_ready");
    expect(post).toHaveBeenCalledWith(
      { type: "load_document", html: "<p>one</p>" },
      "*",
    );
    expect(onLoad).not.toHaveBeenCalled();
    say(target, "document_loaded");
    expect(onLoad).toHaveBeenCalledTimes(1);
  });

  it("ignores messages from any other window", () => {
    const onLoad = vi.fn();
    render(<SandboxFrame title="slide" html="<p>one</p>" onLoad={onLoad} />);
    const post = vi.spyOn(frameOf().contentWindow!, "postMessage");
    say(window, "frame_ready");
    say(null, "document_loaded");
    expect(post).not.toHaveBeenCalled();
    expect(onLoad).not.toHaveBeenCalled();
  });

  it("sends the latest document after it changes and the frame reloads", () => {
    const { rerender } = render(
      <SandboxFrame title="slide" html="<p>one</p>" />,
    );
    rerender(<SandboxFrame title="slide" html="<p>two</p>" />);
    const target = frameOf().contentWindow!;
    const post = vi.spyOn(target, "postMessage");
    say(target, "frame_ready");
    expect(post).toHaveBeenCalledWith(
      { type: "load_document", html: "<p>two</p>" },
      "*",
    );
  });

  it("forwards the ref", () => {
    const ref = { current: null as HTMLIFrameElement | null };
    render(<SandboxFrame ref={ref} title="slide" html="" />);
    expect(ref.current).toBe(frameOf());
  });
});
