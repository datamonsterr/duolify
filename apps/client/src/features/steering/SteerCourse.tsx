import { closeOnEscape } from "../hotkeys/escape";
import { useEffect, useRef, useState } from "react";
import { guidedEnabled } from "../guided-chat/flags";
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
    const onKey = (event: KeyboardEvent) =>
      closeOnEscape(
        event,
        () => {
          setOpen(false);
          trigger.current?.focus();
        },
        "learn",
      );
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, mobile]);
  function close() {
    setOpen(false);
    trigger.current?.focus();
  }
  return (
    <div className="steering-workspace">
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
            <InstructionPanel
              courseId={courseId}
              guided={guidedEnabled("course_steer")}
            />
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
