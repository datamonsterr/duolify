import { useEffect, useId, useRef } from "react";
import { createPortal } from "react-dom";
import { useUiPreferences } from "../design/localization/Preferences";
import { Icon } from "../design/Icon";
import type { ScreenId } from "../shell/navigation";
import { useEscapeLayer } from "./escape";
import { shortcutMessage } from "./messages";
import {
  getHelpSections,
  shortcutKeys,
  shortcutPlatform,
  type ShortcutScope,
} from "./registry";
import { ShortcutButton } from "./ShortcutButton";

export default function ShortcutHelp({
  close,
  desktop,
  scope,
  surface,
}: {
  close: () => void;
  desktop: boolean;
  scope: ShortcutScope;
  surface: ScreenId;
}) {
  const { preferences } = useUiPreferences();
  const dialog = useRef<HTMLDialogElement>(null);
  const heading = useId();
  const t = (id: Parameters<typeof shortcutMessage>[1]) =>
    shortcutMessage(preferences.locale, id);
  useEffect(() => {
    const node = dialog.current!;
    const previous = document.activeElement;
    if (typeof node.showModal === "function") node.showModal();
    else node.setAttribute("open", "");
    node.querySelector("button")?.focus();
    return () => {
      if (typeof node.close === "function" && node.open) node.close();
      if (previous instanceof HTMLElement && previous.isConnected)
        previous.focus();
    };
  }, []);
  useEscapeLayer("sheet", close, surface, () => dialog.current);

  return createPortal(
    <dialog
      className="shortcut-help"
      ref={dialog}
      aria-labelledby={heading}
      onCancel={(event) => {
        event.preventDefault();
        close();
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) close();
      }}
      onKeyDown={(event) => {
        if (event.key === "Tab") {
          event.preventDefault();
          dialog.current?.querySelector("button")?.focus();
        }
      }}
    >
      <div className="shortcut-help-content">
        <header className="shortcut-help-header">
          <h2 id={heading}>{t("show_help")}</h2>
          <ShortcutButton
            id="dismiss"
            label={<Icon name="close" />}
            ariaLabel={t("dismiss")}
            className="shortcut-help-close"
            surface={surface}
            onClick={close}
          />
        </header>
        <div className="shortcut-help-groups">
          {getHelpSections(scope, desktop).map(({ group, rows }) => (
            <section key={group}>
              <h3>{t(group)}</h3>
              <dl>
                {rows.map(([first, ...rest]) => (
                  <div className="shortcut-help-row" key={first.id}>
                    <dt>{t(first.label)}</dt>
                    <dd dir="ltr">
                      {[first, ...rest].flatMap(({ id }) =>
                        shortcutKeys(id, shortcutPlatform()).map((key) => (
                          <kbd key={`${id}:${key}`}>{key}</kbd>
                        )),
                      )}
                    </dd>
                  </div>
                ))}
              </dl>
            </section>
          ))}
        </div>
      </div>
    </dialog>,
    document.body,
  );
}
