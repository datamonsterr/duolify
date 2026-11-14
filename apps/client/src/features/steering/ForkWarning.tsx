import { useEffect, useId, useRef } from "react";
import { Button } from "../design/Button";

const FOCUSABLE = "button:not([disabled])";

const keptCopy = (kept: number) =>
  kept === 1
    ? "Lesson 1 stays as it is."
    : `Lessons 1–${kept} stay as they are.`;

/**
 * Warning shown before the first instruction on a published course copy (E16-05).
 * Desktop dialog, mobile bottom sheet. Escape or Cancel declines; focus stays inside.
 */
export function ForkWarning({
  keptLessons,
  lessonCredits,
  onCancel,
  onConfirm,
}: {
  keptLessons: number;
  lessonCredits?: number;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const titleId = useId();
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    root.current?.focus();
  }, []);
  function keys(event: React.KeyboardEvent) {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      onCancel();
    } else if (event.key === "Tab" && root.current) {
      const items = [...root.current.querySelectorAll<HTMLElement>(FOCUSABLE)];
      const first = items[0];
      const last = items[items.length - 1];
      if (!first || !last) return;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }
  }
  return (
    <div className="steering-fork-backdrop">
      <div
        ref={root}
        className="steering-fork"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        onKeyDown={keys}
      >
        <h2 id={titleId}>Make this course your own?</h2>
        <p>
          This course is shared by its publisher. Steering it creates your
          personal version from lesson {keptLessons + 1} onward.
        </p>
        <ul>
          {keptLessons > 0 && <li>{keptCopy(keptLessons)}</li>}
          <li>
            From lesson {keptLessons + 1}, lessons are prepared for you and{" "}
            {lessonCredits
              ? `cost about ${lessonCredits} credits each`
              : "cost credits at the current lesson price"}
            .
          </li>
          <li>You won&apos;t receive the publisher&apos;s future updates.</li>
        </ul>
        <div className="steering-actions">
          <Button variant="secondary" onClick={onCancel}>
            Cancel
          </Button>
          <Button onClick={onConfirm}>Make it mine</Button>
        </div>
      </div>
    </div>
  );
}
