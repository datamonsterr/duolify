import { useUiPreferences } from "../localization/Preferences";
import {
  useEffect,
  useEffectEvent,
  useId,
  useRef,
  type ReactNode,
  type RefObject,
} from "react";
import { trackGuidedUi, type GuidedSheetId } from "../../../lib/analytics";
import { Button } from "../Button";
import "../chat/guided-chat.css";

export type SheetProps = {
  open: boolean;
  onClose: () => void;
  title: string;
  sheetId: GuidedSheetId;
  children: ReactNode;
  onPausePlayer?: () => void;
  modal?: boolean;
  returnFocusRef?: RefObject<HTMLButtonElement | null>;
};
const focusable =
  'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex="0"]';

export function Sheet({
  open,
  onClose,
  title,
  sheetId,
  children,
  onPausePlayer,
  modal = true,
  returnFocusRef,
}: SheetProps) {
  const { t } = useUiPreferences();
  const dialog = useRef<HTMLDialogElement>(null);
  const reportedOpen = useRef(false);
  const editorFocus = useRef<HTMLElement | null>(null);
  const titleId = useId();
  const onOpened = useEffectEvent(() => onPausePlayer?.());
  useEffect(() => {
    const node = dialog.current;
    if (!node) return;
    if (!open || !modal) {
      reportedOpen.current = false;
      // Modal cleanup closes the native dialog after React sets the open prop.
      // Re-establish the docked presentation and editing focus after that cleanup.
      if (open && !modal) {
        node.setAttribute("open", "");
        editorFocus.current?.focus();
        editorFocus.current = null;
      }
      return;
    }
    const previous =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    const returnTarget = returnFocusRef?.current ?? previous;
    if (!node.open) {
      if (typeof node.showModal === "function") node.showModal();
      else node.setAttribute("open", ""); // Non-browser platform doubles.
    }
    if (!reportedOpen.current) {
      reportedOpen.current = true;
      onOpened();
      trackGuidedUi({ event: "ui_sheet_opened", sheetId });
    }
    node.querySelector<HTMLElement>(focusable)?.focus();
    return () => {
      const active = document.activeElement;
      if (
        active instanceof HTMLElement &&
        node.contains(active) &&
        active.tagName !== "BUTTON"
      )
        editorFocus.current = active;
      if (typeof node.close === "function" && node.open) node.close();
      else node.removeAttribute("open");
      if (returnTarget?.isConnected && !node.contains(returnTarget))
        returnTarget.focus();
    };
  }, [open, modal, sheetId, returnFocusRef]);
  function close() {
    trackGuidedUi({
      event: "ui_control_activated",
      surface: "sheet",
      controlId: "close",
    });
    onClose();
  }
  return (
    <dialog
      ref={dialog}
      open={!modal && open ? true : undefined}
      className={`guided-sheet ${modal ? "modal" : "docked"}`}
      role={modal ? "dialog" : "complementary"}
      aria-modal={modal ? true : undefined}
      aria-labelledby={titleId}
      onCancel={(event) => {
        event.preventDefault();
        close();
      }}
      onKeyDown={(event) => {
        if (!modal || !open) return;
        if (event.key === "Escape") {
          event.preventDefault();
          close();
        }
        if (event.key === "Tab") {
          const nodes = [
            ...(dialog.current?.querySelectorAll<HTMLElement>(focusable) ?? []),
          ].filter((node) => !node.hidden && !node.closest("[hidden]"));
          const first = nodes[0],
            last = nodes[nodes.length - 1];
          if (event.shiftKey && document.activeElement === first) {
            event.preventDefault();
            last?.focus();
          } else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault();
            first?.focus();
          }
        }
      }}
    >
      <header className="guided-panel-header">
        <h2 id={titleId}>{title}</h2>
        {modal && (
          <Button
            variant="quiet"
            onClick={close}
            aria-label={t("common.close_named", { name: title })}
          >
            {t("common.close")}
          </Button>
        )}
      </header>
      {children}
    </dialog>
  );
}
