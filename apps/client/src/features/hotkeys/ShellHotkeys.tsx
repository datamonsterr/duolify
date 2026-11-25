import { isTauri } from "@tauri-apps/api/core";
import { lazy, Suspense, useEffect } from "react";
import { useUiPreferences } from "../design/localization/Preferences";
import { navigateTo } from "../../lib/navigation";
import { trackHotkey, trackNavigation } from "../../lib/analytics";
import { clientPlatform } from "../../lib/client-build";
import {
  mobileTabs,
  screenForPath,
  type Destination,
} from "../shell/navigation";
import { isTextTarget } from "./guards";
import { useHotkeys } from "./useHotkeys";
import {
  nativeMenu,
  shortcutAria,
  shortcutKeys,
  shortcutPlatform,
  type ShortcutId,
} from "./registry";

const ShortcutHelp = lazy(() => import("./ShortcutHelp"));

export default function ShellHotkeys({
  path,
  open,
  setOpen,
}: {
  path: string;
  open: boolean;
  setOpen: (open: boolean) => void;
}) {
  const { t, preferences } = useUiPreferences();
  const surface = screenForPath(path);
  const desktop =
    clientPlatform(isTauri(), import.meta.env.VITE_MOBILE_PLATFORM) ===
    "desktop";

  useHotkeys({
    scope: "app",
    surface,
    handlers: {
      undo: () => {
        const button = document.querySelector<HTMLButtonElement>(
          '[data-testid="toast-action"]',
        );
        if (
          !button?.getClientRects().length ||
          button.textContent?.trim() !== t("common.undo")
        )
          return false;
        button.click();
      },
    },
  });

  useEffect(() => {
    if (!desktop) return;
    let disposed = false;
    let unlisten: (() => void) | undefined;
    void import("@tauri-apps/api/event")
      .then(({ listen }) =>
        disposed
          ? undefined
          : listen<string>("duolify-hotkey", ({ payload }) => {
              if (disposed) return;
              const item = nativeMenu.find(({ id }) => id === payload);
              if (!item) return;
              if (item.id === "show_help") {
                if (!isTextTarget()) {
                  setOpen(true);
                  trackHotkey(item.id, surface, "button");
                }
                return;
              }
              const destination = nativeDestination(item.id);
              if (!destination) return;
              if (!isTextTarget()) trackHotkey(item.id, surface, "button");
              trackNavigation(destination.id);
              navigateTo(destination.to);
            }),
      )
      .then((cleanup) => {
        if (disposed) cleanup?.();
        else unlisten = cleanup;
      })
      .catch(() => {
        // Browser fixtures may provide a native credential store without Tauri events.
      });
    return () => {
      disposed = true;
      unlisten?.();
    };
  }, [desktop, surface, setOpen]);

  useEffect(() => {
    if (!desktop) return;
    const links: HTMLAnchorElement[] = [];
    const platform = shortcutPlatform();
    for (const item of nativeMenu) {
      const target = nativeDestination(item.id);
      if (!target) continue;
      for (const link of document.querySelectorAll<HTMLAnchorElement>(
        `a[data-tour="${target.id}"]`,
      )) {
        const label = link.textContent?.trim();
        const keys = shortcutAria(item.id, platform);
        if (keys) {
          link.setAttribute("aria-keyshortcuts", keys);
          link.title = `${label} (${shortcutKeys(item.id, platform)[0]})`;
          links.push(link);
        }
      }
    }
    return () =>
      links.forEach((link) => {
        link.removeAttribute("aria-keyshortcuts");
        link.removeAttribute("title");
      });
  }, [desktop, path, preferences.locale]);

  return (
    <>
      {open && (
        <Suspense fallback={null}>
          <ShortcutHelp
            desktop={desktop}
            scope={
              surface === "lesson"
                ? "player"
                : surface === "lesson_test"
                  ? "lesson_test"
                  : "app"
            }
            surface={surface}
            close={() => setOpen(false)}
          />
        </Suspense>
      )}
    </>
  );
}

function nativeDestination(
  id: ShortcutId,
): { id: Destination; to: string } | undefined {
  if (id === "settings") return { id, to: "/settings" };
  return mobileTabs.slice(0, 4).find((item) => id === `go_${item.id}`);
}
