import { fireEvent, render, screen } from "@testing-library/react";
import { useRef } from "react";
import { describe, expect, it, vi } from "vitest";
import { useSwipeClose } from "./useSwipeClose";

function Panel({
  onClose,
  dir,
  rotated,
}: {
  onClose: () => void;
  dir?: "rtl";
  rotated?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useSwipeClose({
    panel: ref,
    grabSelector: ".grab",
    enabled: true,
    onClose,
    reducedMotion: false,
    rotated,
  });
  return (
    <div ref={ref} dir={dir} data-testid="panel">
      <span className="grab" data-testid="grab" />
      <div data-testid="log" />
    </div>
  );
}
Element.prototype.getBoundingClientRect = () =>
  ({ width: 360, height: 300 }) as DOMRect;
const ev = (id: number, x: number, y: number, t: number) => ({
  pointerId: id,
  clientX: x,
  clientY: y,
  timeStamp: t,
});
function drag(el: Element, from: number, to: number, y = 0) {
  fireEvent.pointerDown(el, ev(1, from, 0, 0));
  fireEvent.pointerMove(el, ev(1, (from + to) / 2, y, 200));
  fireEvent.pointerMove(el, ev(1, to, y, 400));
  fireEvent.pointerUp(el, ev(1, to, y, 600));
}

describe("useSwipeClose rotated stage", () => {
  const swipe = (el: Element, dx: number, dy: number) => {
    fireEvent.pointerDown(el, ev(1, 100, 100, 0));
    fireEvent.pointerMove(el, ev(1, 100 + dx / 2, 100 + dy / 2, 200));
    fireEvent.pointerMove(el, ev(1, 100 + dx, 100 + dy, 400));
    fireEvent.pointerUp(el, ev(1, 100 + dx, 100 + dy, 600));
  };
  it("closes on a downward screen swipe (toward the docked bottom edge)", () => {
    const onClose = vi.fn();
    render(<Panel onClose={onClose} rotated />);
    swipe(screen.getByTestId("grab"), 0, 200);
    expect(onClose).toHaveBeenCalledTimes(1);
  });
  it("ignores a sideways screen swipe and an upward one", () => {
    const onClose = vi.fn();
    render(<Panel onClose={onClose} rotated />);
    swipe(screen.getByTestId("grab"), 200, 0);
    swipe(screen.getByTestId("grab"), 0, -200);
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("useSwipeClose", () => {
  it("closes on a long swipe toward the edge from the grab area", () => {
    const onClose = vi.fn();
    render(<Panel onClose={onClose} />);
    drag(screen.getByTestId("grab"), 0, 200);
    expect(onClose).toHaveBeenCalledTimes(1);
  });
  it("springs back on a short slow drag and never starts from the log", () => {
    const onClose = vi.fn();
    render(<Panel onClose={onClose} />);
    drag(screen.getByTestId("grab"), 0, 20);
    drag(screen.getByTestId("log"), 0, 300);
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByTestId("panel").style.transform).toBe("");
  });
  it("ignores a mostly vertical drag", () => {
    const onClose = vi.fn();
    render(<Panel onClose={onClose} />);
    drag(screen.getByTestId("grab"), 0, 60, 200);
    expect(onClose).not.toHaveBeenCalled();
  });
  it("mirrors in RTL: closes toward the left only", () => {
    const onClose = vi.fn();
    render(<Panel onClose={onClose} dir="rtl" />);
    drag(screen.getByTestId("grab"), 300, 100);
    expect(onClose).toHaveBeenCalledTimes(1);
    drag(screen.getByTestId("grab"), 0, 300);
    expect(onClose).toHaveBeenCalledTimes(1);
  });
  it("a second finger cancels", () => {
    const onClose = vi.fn();
    render(<Panel onClose={onClose} />);
    const grab = screen.getByTestId("grab");
    fireEvent.pointerDown(grab, ev(1, 0, 0, 0));
    fireEvent.pointerDown(document.body, ev(2, 500, 100, 50));
    fireEvent.pointerMove(grab, ev(1, 300, 0, 200));
    fireEvent.pointerUp(grab, ev(1, 300, 0, 300));
    expect(onClose).not.toHaveBeenCalled();
  });
});
