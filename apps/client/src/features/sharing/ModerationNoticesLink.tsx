import { Link, useRouterState } from "@tanstack/react-router";
import { Badge } from "../design/Badge";
import { useUiPreferences } from "../design/localization/Preferences";
import { useModerationNotices } from "./moderation";

export function ModerationNoticesLink({
  className = "",
}: {
  className?: string;
}) {
  const { t } = useUiPreferences();
  const notices = useModerationNotices();
  const path = useRouterState({ select: (state) => state.location.pathname });

  if (notices.length === 0) return null;

  const attentionCount = notices.filter(
    (n) => n.can_appeal || n.appeal?.status === "open",
  ).length;

  const isCurrent =
    path === "/moderation/notices" || path.startsWith("/moderation/notices/");

  const label = t("nav.moderation_notices");

  return (
    <Link
      to="/moderation/notices"
      className={className}
      activeOptions={{ exact: true }}
      aria-current={isCurrent ? "page" : undefined}
      aria-label={attentionCount > 0 ? `${label} (${attentionCount})` : label}
    >
      <svg
        className="nav-icon nav-icon-warning"
        viewBox="0 0 24 24"
        aria-hidden="true"
      >
        <path d="M5 21V3h13l-3 5 3 5H5" />
      </svg>
      <span>{label}</span>
      {attentionCount > 0 && <Badge role="warning">{attentionCount}</Badge>}
    </Link>
  );
}
