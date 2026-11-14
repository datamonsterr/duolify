import { DailyGoal } from "../gamification/DailyGoal";
import { PreferenceControls } from "../design/localization/PreferenceControls";
import { useUiPreferences } from "../design/localization/Preferences";
import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import { useEffect, useLayoutEffect, useRef } from "react";
import { trackNavigation, trackScreen } from "../../lib/analytics";
import { Badge } from "../design/Badge";
import {
  accountNavigation,
  currentDestination,
  learningNavigation,
  mobileTabs,
  moreDestinations,
  screenForPath,
  type Destination,
} from "./navigation";
import { useMobileLayout } from "./AdaptiveLayouts";
import { ShellSession } from "./ShellSession";
import { useMarketplaceEnabled } from "../../lib/features";
import { ModerationNoticesLink } from "../sharing/ModerationNoticesLink";

const icons: Record<string, string> = {
  home: "M3 11 12 4l9 7v9h-6v-6H9v6H3z",
  courses: "m2 8 10-5 10 5-10 5z M2 13l10 5 10-5 M2 18l10 5 10-5",
  friends:
    "M8 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8 M2 21c0-8 12-8 12 0 M17 4a4 4 0 0 1 0 8 M17 15c4 0 5 3 5 6",
  flag: "M5 21V3h13l-3 5 3 5H5",
  note: "M6 3h9l4 4v14H6z M14 3v5h5 M9 13h7 M9 17h5",
  bookmark: "M6 3h12v18l-6-4-6 4z",
  gem: "M7 4h10l4 5-9 11L3 9z M3 9h18 M10 4 8 9l4 11 4-11-2-5",
  profile: "M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8 M4 21c0-9 16-9 16 0",
  settings: "M4 7h16 M4 17h16 M8 4v6 M16 14v6",
  more: "M4 6h16 M4 12h16 M4 18h16",
};
const iconAccents: Record<string, string> = {
  home: "primary",
  courses: "secondary",
  note: "secondary",
  gem: "credit-icon",
  profile: "muted",
  settings: "muted",
  flag: "warning",
};
function NavIcon({ name }: { name: string }) {
  return (
    <svg
      className={`nav-icon nav-icon-${iconAccents[name] ?? "secondary"}`}
      viewBox="0 0 24 24"
      aria-hidden="true"
    >
      <path d={icons[name]} />
    </svg>
  );
}
function Brand() {
  return (
    <Link className="brand" to="/">
      <span className="brand-mark" aria-hidden="true">
        <span />
        <span />
      </span>
      duolify
    </Link>
  );
}
function DestinationLink({
  item,
  mobile = false,
}: {
  item: { id: Destination; label: string; to: string; icon: string };
  mobile?: boolean;
}) {
  const { t } = useUiPreferences();
  const path = useRouterState({ select: (state) => state.location.pathname });
  return (
    <Link
      to={item.to}
      activeOptions={{ exact: true }}
      aria-current={
        currentDestination(path, mobile) === item.id ? "page" : undefined
      }
      onClick={() => trackNavigation(item.id)}
    >
      <NavIcon name={item.icon} />
      <span>
        {t(
          item.id === "notes" && mobile ? "nav.notes_short" : `nav.${item.id}`,
        )}
      </span>
    </Link>
  );
}
/** The Marketplace entry exists only while the release flag is on (E15-07). */
function MarketplaceLink() {
  const { t } = useUiPreferences();
  const path = useRouterState({ select: (state) => state.location.pathname });
  const enabled = useMarketplaceEnabled();
  if (!enabled) return null;
  return (
    <Link
      to="/marketplace"
      aria-current={
        path === "/marketplace" || path.startsWith("/marketplace/")
          ? "page"
          : undefined
      }
    >
      <NavIcon name="courses" />
      <span>{t("nav.marketplace")}</span>
    </Link>
  );
}
export function MorePage() {
  const { t } = useUiPreferences();
  return (
    <>
      <h1 tabIndex={-1}>{t("nav.more")}</h1>
      <nav className="more-links" aria-label={t("shell.more")}>
        {moreDestinations.map((item) => (
          <DestinationLink key={item.id} item={item} />
        ))}
        <MarketplaceLink />
        <ModerationNoticesLink />
      </nav>
    </>
  );
}

export function AppShell() {
  const { t } = useUiPreferences();
  const path = useRouterState({ select: (state) => state.location.pathname });
  const status = useRouterState({ select: (state) => state.status });
  const mobile = useMobileLayout();
  const focused = /^\/lesson\/[^/]+(?:\/test)?\/?$/.test(path);
  const main = useRef<HTMLElement>(null);
  const positions = useRef(new Map<string, number>());
  const lastScreen = useRef<string | null>(null);
  useLayoutEffect(() => {
    const node = main.current!;
    const position = positions.current.get(path) ?? 0;
    node.scrollTop = position;
    const remember = () => positions.current.set(path, node.scrollTop);
    node.addEventListener("scroll", remember);
    return () => node.removeEventListener("scroll", remember);
  }, [path]);
  useEffect(() => {
    const node = main.current!;
    let complete = false;
    const focus = () => {
      const heading = node.querySelector<HTMLElement>("h1");
      if (!heading || complete) return;
      heading.tabIndex = -1;
      heading.focus({ preventScroll: true });
      node.scrollTop = positions.current.get(path) ?? 0;
      complete = true;
      if (status === "idle" && lastScreen.current !== path) {
        trackScreen(screenForPath(path));
        lastScreen.current = path;
      }
    };
    focus();
    const observer = new MutationObserver(focus);
    observer.observe(node, { childList: true, subtree: true });
    return () => observer.disconnect();
  }, [path, status]);
  return (
    <ShellSession>
      <div className={`app-frame${focused ? " focused-frame" : ""}`}>
        <a href="#main-content" className="skip-link">
          {t("shell.skip")}
        </a>
        {!focused && (
          <>
            <aside className="sidebar">
              <Brand />
              {!mobile && (
                <nav aria-label={t("shell.main")}>
                  <div role="group" aria-label={t("shell.learning")}>
                    {learningNavigation.map((item) => (
                      <DestinationLink key={item.id} item={item} />
                    ))}
                    <MarketplaceLink />
                  </div>
                  <hr role="separator" />
                  <div role="group" aria-label={t("shell.account")}>
                    {accountNavigation.map((item) => (
                      <DestinationLink key={item.id} item={item} />
                    ))}
                    <ModerationNoticesLink />
                  </div>
                </nav>
              )}
              <p className="sidebar-foot">{t("shell.motto")}</p>
            </aside>
            <header className="mobile-header">
              <Brand />
              <Link
                className="credit-shortcut"
                to="/credits"
                onClick={() => trackNavigation("credits")}
              >
                {t("nav.credits")}{" "}
                <span className="muted">{t("shell.balance")}</span>
              </Link>
            </header>
          </>
        )}
        <main className="app-main" id="main-content" tabIndex={-1} ref={main}>
          {path !== "/settings" && (
            <details className="appearance-menu">
              <summary>{t("settings.title")}</summary>
              <PreferenceControls />
            </details>
          )}
          <Outlet />
        </main>
        {!focused && (
          <>
            <aside className="support-rail" aria-label={t("shell.progress")}>
              <section className="card">
                <h2>{t("nav.streak")}</h2>
                <Badge role="streak">{t("shell.preview")}</Badge>
                <p>{t("shell.streak_example")}</p>
                <progress
                  className="support-progress"
                  value={3}
                  max={7}
                  aria-label={t("shell.streak_example_label")}
                />
                <Link
                  className="shell-link"
                  to="/streak"
                  onClick={() => trackNavigation("streak")}
                >
                  {t("shell.streak")}
                </Link>
              </section>
              {path !== "/streak" && (
                <section className="card">
                  <DailyGoal compact />
                  <Link
                    className="shell-link"
                    to="/streak"
                    onClick={() => trackNavigation("streak")}
                  >
                    {t("shell.goal_settings")}
                  </Link>
                </section>
              )}

              <section className="card">
                <h2>Credit options</h2>
                <Badge role="credit">Balance unavailable</Badge>
                <p>Check your balance and available credits.</p>
                <Link
                  className="shell-link"
                  to="/credits"
                  onClick={() => trackNavigation("credits")}
                >
                  View credits
                </Link>
              </section>
            </aside>
            {mobile && (
              <nav className="bottom-tabs" aria-label={t("shell.main")}>
                {mobileTabs.map((item) => (
                  <DestinationLink key={item.id} item={item} mobile />
                ))}
              </nav>
            )}
          </>
        )}
      </div>
    </ShellSession>
  );
}
