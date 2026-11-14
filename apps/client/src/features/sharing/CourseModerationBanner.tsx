import { Link } from "@tanstack/react-router";
import { useUiPreferences } from "../design/localization/Preferences";
import { useModerationNotices } from "./moderation";
import "./moderation.css";

export function CourseModerationBanner({ courseId }: { courseId: string }) {
  const { t } = useUiPreferences();
  const notices = useModerationNotices();

  const courseNotices = notices.filter((n) => n.course_id === courseId);
  if (courseNotices.length === 0) return null;

  const needsAttention = courseNotices.some(
    (n) => n.can_appeal || n.appeal?.status === "open",
  );

  return (
    <div
      className="ban warn mod-banner"
      role="status"
      data-testid="course-moderation-banner"
    >
      <svg
        className="node-icon mod-banner-icon"
        aria-hidden="true"
        viewBox="0 0 24 24"
      >
        <path d="M12 9v4 M12 17h.01 M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
      </svg>
      <div>
        <b>{t("moderation.course_banner_title")}</b>
        <p>
          {needsAttention
            ? t("moderation.course_banner_attention")
            : t("moderation.course_banner_generic")}{" "}
          <Link to="/moderation/notices" className="shell-link mod-banner-link">
            {t("moderation.course_banner_link")}
          </Link>
        </p>
      </div>
    </div>
  );
}
