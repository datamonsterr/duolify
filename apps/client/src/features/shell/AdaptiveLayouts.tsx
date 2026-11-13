import {
  useEffect,
  useId,
  useRef,
  useSyncExternalStore,
  type ReactNode,
} from "react";

const query = "(width < 48rem)";
function subscribe(callback: () => void) {
  const media = window.matchMedia?.(query);
  media?.addEventListener("change", callback);
  return () => media?.removeEventListener("change", callback);
}
export function useMobileLayout() {
  return useSyncExternalStore(
    subscribe,
    () => window.matchMedia?.(query).matches ?? false,
    () => false,
  );
}

// Children remain in one DOM tree. Feature owners retain business/draft state.
export function ResponsivePanel({
  title,
  open,
  onClose,
  children,
  mobilePresentation = "sheet",
}: {
  title: string;
  open: boolean;
  onClose: () => void;
  children: ReactNode;
  mobilePresentation?: "sheet" | "step";
}) {
  const mobile = useMobileLayout();
  const id = useId();
  const panel = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLElement | null>(null);
  const close = useRef(onClose);
  useEffect(() => {
    close.current = onClose;
  }, [onClose]);
  useEffect(() => {
    if (!open) return;
    trigger.current =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    const target = trigger.current;
    return () => {
      queueMicrotask(() => {
        if (target?.isConnected) target.focus({ preventScroll: true });
      });
    };
  }, [open]);
  useEffect(() => {
    if (!open || !mobile) return;
    const node = panel.current!;
    const inert: { node: Element; wasInert: boolean }[] = [];
    let branch: Element = node;
    while (branch.parentElement) {
      for (const sibling of branch.parentElement.children) {
        if (sibling !== branch) {
          inert.push({
            node: sibling,
            wasInert: sibling.hasAttribute("inert"),
          });
          sibling.setAttribute("inert", "");
        }
      }
      branch = branch.parentElement;
    }
    const controls = () =>
      [
        ...node.querySelectorAll<HTMLElement>(
          'a[href], button:not(:disabled), textarea:not(:disabled), input:not(:disabled), select:not(:disabled), [tabindex="0"]',
        ),
      ].filter((el) => !el.closest("[hidden]"));
    const focusFirst = () =>
      (controls()[0] ?? node).focus({ preventScroll: true });
    if (!node.contains(document.activeElement)) focusFirst();
    const contain = (event: FocusEvent) => {
      if (!node.contains(event.target as Node)) focusFirst();
    };
    const keyboard = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        close.current();
      }
      if (event.key === "Tab") {
        const items = controls(),
          first = items[0],
          last = items[items.length - 1];
        if (!first) {
          event.preventDefault();
          node.focus();
        } else if (
          event.shiftKey &&
          (document.activeElement === first || document.activeElement === node)
        ) {
          event.preventDefault();
          last?.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first.focus();
        }
      }
    };
    document.addEventListener("focusin", contain);
    node.addEventListener("keydown", keyboard);
    return () => {
      document.removeEventListener("focusin", contain);
      node.removeEventListener("keydown", keyboard);
      for (const item of inert)
        if (!item.wasInert) item.node.removeAttribute("inert");
    };
  }, [open, mobile, mobilePresentation]);
  return (
    <div
      className={`panel-host panel-${mobilePresentation}`}
      hidden={!open}
      onClick={(event) => {
        if (
          event.target === event.currentTarget &&
          mobile &&
          mobilePresentation === "sheet"
        )
          onClose();
      }}
    >
      <div
        ref={panel}
        className="adaptive-panel"
        role={mobile && mobilePresentation === "sheet" ? "dialog" : "region"}
        aria-modal={mobile && mobilePresentation === "sheet" ? true : undefined}
        aria-labelledby={id}
        tabIndex={-1}
      >
        <header className="panel-heading">
          <h2 id={id}>{title}</h2>
          <button
            className="duo-button quiet"
            onClick={onClose}
            aria-label={`Close ${title}`}
          >
            Close
          </button>
        </header>
        {children}
      </div>
    </div>
  );
}

export function PanelLayout({
  children,
  panel,
}: {
  children: ReactNode;
  panel: ReactNode;
}) {
  return (
    <div className="panel-layout">
      <div className="panel-main">{children}</div>
      {panel}
    </div>
  );
}

export function ListDetailLayout({
  list,
  detail,
  detailActive,
  back,
}: {
  list: ReactNode;
  detail: ReactNode;
  detailActive: boolean;
  back: ReactNode;
}) {
  const mobile = useMobileLayout();
  return (
    <div className="list-detail-layout">
      <div hidden={mobile && detailActive}>{list}</div>
      <div hidden={mobile && !detailActive}>
        {mobile && back}
        {detail}
      </div>
    </div>
  );
}
