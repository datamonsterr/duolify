import { useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { trackGuidedUi, type GuidedSheetId } from "../../../lib/analytics";
import { Sheet } from "./Sheet";

const query = "(max-width: 47.999rem)";
function subscribe(notify: () => void) {
  if (!window.matchMedia) return () => {};
  const media = window.matchMedia(query);
  media.addEventListener("change", notify);
  return () => media.removeEventListener("change", notify);
}
function snapshot() {
  return window.matchMedia?.(query).matches ?? false;
}
export function ResponsivePanel({
  title,
  triggerLabel,
  sheetId,
  children,
  onPausePlayer,
}: {
  title: string;
  triggerLabel: string;
  sheetId: GuidedSheetId;
  children: ReactNode;
  onPausePlayer?: () => void;
}) {
  const mobile = useSyncExternalStore(subscribe, snapshot, () => false);
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  return (
    <div className="guided-responsive-panel">
      <button
        type="button"
        ref={trigger}
        className="duo-button secondary guided-panel-trigger"
        aria-haspopup="dialog"
        aria-expanded={mobile && open}
        onClick={() => {
          trackGuidedUi({
            event: "ui_control_activated",
            surface: "sheet",
            controlId: "open",
          });
          setOpen(true);
        }}
      >
        {triggerLabel}
      </button>
      <Sheet
        open={!mobile || open}
        modal={mobile}
        title={title}
        sheetId={sheetId}
        onClose={() => setOpen(false)}
        onPausePlayer={onPausePlayer}
        returnFocusRef={trigger}
      >
        {children}
      </Sheet>
    </div>
  );
}
