import { OnboardingGate } from "../onboarding/OnboardingGate";
import { DailyGoal } from "../gamification/DailyGoal";
import { PreferenceControls } from "../design/localization/PreferenceControls";
import { useUiPreferences } from "../design/localization/Preferences";
import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import { useEffect, useLayoutEffect, useRef } from "react";
import { trackNavigation, trackScreen } from "../../lib/analytics";
import {
  RailCreditsCard,
  RailStreakCard,
  useBalanceLabel,
  useWallet,
} from "./RailCards";
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
import { Brand } from "../design/brand/Brand";
import { Icon, type IconName } from "../design/Icon";

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
    <Icon name={name as IconName} accent={iconAccents[name] ?? "secondary"} />
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
      data-tour={item.id}
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
  // Refreshed on every navigation so spends and purchases show up in the rail.
  const { data: wallet } = useWallet(path);
  const balance = useBalanceLabel(wallet);
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
              <Brand to="/" />
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
              <Brand to="/" />
              <Link
                data-tour="credits"
                className="credit-shortcut"
                to="/credits"
                onClick={() => trackNavigation("credits")}
              >
                {t("nav.credits")} <span className="muted">{balance}</span>
              </Link>
            </header>
          </>
        )}
        <main className="app-main" id="main-content" tabIndex={-1} ref={main}>
          {path !== "/settings" && !mobile && (
            <details className="appearance-menu">
              <summary>{t("settings.title")}</summary>
              <PreferenceControls />
            </details>
          )}
          <Outlet />
          <OnboardingGate path={path} />
        </main>
        {!focused && (
          <>
            <aside className="support-rail" aria-label={t("shell.progress")}>
              <RailStreakCard revision={path} />
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

              <RailCreditsCard wallet={wallet} />
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
