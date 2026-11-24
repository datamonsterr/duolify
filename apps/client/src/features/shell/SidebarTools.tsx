import { useEffect, useId, useRef, useState } from "react";
import { trackHotkey } from "../../lib/analytics";
import { Icon } from "../design/Icon";
import {
  useUiPreferences,
  type Preferences,
} from "../design/localization/Preferences";
import { useEscapeLayer } from "../hotkeys/escape";
import { shortcutMessage } from "../hotkeys/messages";
import { ShortcutButton } from "../hotkeys/ShortcutButton";
import type { ScreenId } from "./navigation";
import "./sidebar-tools.css";

const locales = ["en", "ar"] as const;

/** Icon-only language menu, theme toggle and shortcuts, pinned to the sidebar bottom. */
export function SidebarTools({
  surface,
  onShowHelp,
}: {
  surface: ScreenId;
  onShowHelp: () => void;
}) {
  const menuId = useId();
  const { preferences, theme, update, t } = useUiPreferences();
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const close = (restoreFocus: boolean) => {
    setOpen(false);
    if (restoreFocus) trigger.current?.focus();
  };
  useEscapeLayer(
    "menu",
    open ? () => close(true) : undefined,
    surface,
    () => root.current,
  );
  useEffect(() => {
    if (!open) return;
    root.current
      ?.querySelector<HTMLElement>(
        '[role="menuitemradio"][aria-checked="true"]',
      )
      ?.focus();
    const away = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", away);
    return () => document.removeEventListener("pointerdown", away);
  }, [open]);
  const label = t("settings.language");
  return (
    <div className="sidebar-tools" ref={root}>
      <div className="sidebar-language">
        <button
          ref={trigger}
          type="button"
          className="sidebar-tool"
          data-control="locale"
          aria-label={label}
          title={label}
          aria-haspopup="menu"
          aria-expanded={open}
          aria-controls={open ? menuId : undefined}
          onClick={() => setOpen((value) => !value)}
        >
          <Icon name="globe" />
        </button>
        {open && (
          <div
            id={menuId}
            role="menu"
            aria-label={label}
            className="sidebar-menu"
            onKeyDown={(event) => {
              const items = [
                ...event.currentTarget.querySelectorAll<HTMLElement>(
                  '[role="menuitemradio"]',
                ),
              ];
              const at = items.indexOf(document.activeElement as HTMLElement);
              if (event.key === "ArrowDown" || event.key === "ArrowUp") {
                event.preventDefault();
                const step = event.key === "ArrowDown" ? 1 : -1;
                items[(at + step + items.length) % items.length]?.focus();
              } else if (event.key === "Tab") close(false);
            }}
          >
            {locales.map((locale) => (
              <button
                key={locale}
                type="button"
                role="menuitemradio"
                aria-checked={preferences.locale === locale}
                onClick={() => {
                  update({ locale: locale as Preferences["locale"] });
                  close(true);
                }}
              >
                <span className="sidebar-menu-check" aria-hidden="true">
                  {preferences.locale === locale && <Icon name="check" />}
                </span>
                {t(`lang.${locale}`)}
              </button>
            ))}
          </div>
        )}
      </div>
      <button
        type="button"
        className="sidebar-tool"
        data-control="theme"
        aria-label={t("shell.dark_theme")}
        aria-pressed={theme === "dark"}
        title={t("shell.dark_theme")}
        onClick={() =>
          update({ appearance: theme === "dark" ? "light" : "dark" })
        }
      >
        <Icon name="theme" />
      </button>
      <ShortcutButton
        id="show_help"
        label={<Icon name="keyboard" />}
        ariaLabel={shortcutMessage(preferences.locale, "help_trigger")}
        className="sidebar-tool"
        surface={surface}
        onClick={() => {
          onShowHelp();
          trackHotkey("show_help", surface, "button");
        }}
      />
    </div>
  );
}
