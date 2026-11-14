import { useEffect, useRef, useState } from "react";
import { Sheet } from "../design/sheet/Sheet";
import { Button } from "../design/Button";
import { InstructionPanel } from "./InstructionPanel";
import { useMobile } from "./useMobile";
import { ReplanPanel, type ReplanState } from "./ReplanPanel";
export function SteerCourse({ courseId }: { courseId: string }) {
  const [open, setOpen] = useState(false);
  const [replan, setReplan] = useState(false);
  const [started, setStarted] = useState(false);
  const [replanState, setReplanState] = useState<ReplanState>({});
  const replanTrigger = useRef<HTMLButtonElement>(null);
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
          <div hidden={replan}>
            <InstructionPanel courseId={courseId} />
            <Button
              ref={replanTrigger}
              variant="secondary"
              onClick={() => {
                setStarted(true);
                setReplan(true);
              }}
            >
              Re-plan remaining outline
            </Button>
          </div>
          {started && (
            <div hidden={!replan}>
              <ReplanPanel
                courseId={courseId}
                initial={replanState}
                onState={setReplanState}
                onBack={() => {
                  setReplan(false);
                  requestAnimationFrame(() => replanTrigger.current?.focus());
                }}
              />
            </div>
          )}
        </Sheet>
      )}
    </div>
  );
}
