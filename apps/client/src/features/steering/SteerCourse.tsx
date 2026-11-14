import { useEffect, useRef, useState } from "react";
import { Sheet } from "../design/sheet/Sheet";
import { Button } from "../design/Button";
import { InstructionPanel } from "./InstructionPanel";
import { useMobile } from "./useMobile";
export function SteerCourse({ courseId }: { courseId: string }) {
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const mobile = useMobile();
  useEffect(() => {
    if (!open || mobile) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !event.defaultPrevented) {
        event.preventDefault();
        setOpen(false);
        trigger.current?.focus();
      }
    };
    document.addEventListener("keydown", closeOnEscape);
    return () => document.removeEventListener("keydown", closeOnEscape);
  }, [open, mobile]);
  function close() {
    setOpen(false);
    trigger.current?.focus();
  }
  return (
    <div
      className="steering-workspace"
      onKeyDown={(e) => {
        if (open && e.key === "Escape") {
          e.preventDefault();
          close();
        }
      }}
    >
      <Button
        variant="secondary"
        ref={trigger}
        aria-expanded={open}
        onClick={() => setOpen(true)}
      >
        Steer course
      </Button>
      {open && (
        <Sheet
          open
          modal={mobile}
          title="Steer your course"
          sheetId="course_steering"
          returnFocusRef={trigger}
          onClose={close}
        >
          <InstructionPanel courseId={courseId} />
        </Sheet>
      )}
    </div>
  );
}
