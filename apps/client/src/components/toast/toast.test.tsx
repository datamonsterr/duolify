import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ToastHost, resetToasts, showToast } from ".";

beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }));
afterEach(() => {
  cleanup();
  act(() => resetToasts());
  vi.useRealTimers();
});

describe("toast", () => {
  it("shows one at a time and queues the rest, auto-dismissing after about 4 s", () => {
    render(<ToastHost />);
    act(() => {
      showToast({ message: "First" });
      showToast({ message: "Second" });
    });
    expect(screen.getByText("First")).toBeTruthy();
    expect(screen.queryByText("Second")).toBeNull();
    act(() => void vi.advanceTimersByTime(4100));
    expect(screen.getByText("Second")).toBeTruthy();
    act(() => void vi.advanceTimersByTime(4100));
    expect(screen.queryByTestId("toast")).toBeNull();
  });

  it("dismisses on tap and runs the single action", () => {
    const onAction = vi.fn();
    render(<ToastHost />);
    act(
      () =>
        void showToast({
          message: "Saved",
          action: { label: "Undo", onAction },
        }),
    );
    fireEvent.click(screen.getByTestId("toast-action"));
    expect(onAction).toHaveBeenCalledOnce();
    expect(screen.queryByTestId("toast")).toBeNull();
    act(() => void showToast({ message: "Tap me" }));
    fireEvent.click(screen.getByTestId("toast-dismiss"));
    expect(screen.queryByTestId("toast")).toBeNull();
  });

  it("is polite, assertive for errors, and does not take focus", () => {
    render(<ToastHost />);
    const host = screen.getByTestId("toast-host");
    expect(host.getAttribute("aria-live")).toBe("polite");
    const before = document.activeElement;
    act(() => void showToast({ message: "Broke", tone: "error" }));
    expect(host.getAttribute("aria-live")).toBe("assertive");
    expect(document.activeElement).toBe(before);
  });

  it("replaces a toast with the same id", () => {
    render(<ToastHost />);
    act(() => {
      showToast({ id: "a", message: "One" });
      showToast({ id: "a", message: "Two" });
    });
    expect(screen.getByText("Two")).toBeTruthy();
  });
});
