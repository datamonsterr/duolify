import { useRef, useState, useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { trackControl } from "../../lib/analytics";
import { useShellSession } from "../shell/ShellSession";
import { Badge } from "./Badge";
import "./courses.css";
import { Button } from "./Button";
import { CourseCard } from "./CourseCard";
import { AppLink, navigateTo } from "../../lib/navigation";
import { ExampleCourses } from "./ExampleCourses";
import { StatIcon } from "./StatIcon";
import { useStreak, useWallet } from "../shell/SidebarStats";
import { useUiPreferences } from "./localization/Preferences";

function subscribeOnline(notify: () => void) {
  window.addEventListener("online", notify);
  window.addEventListener("offline", notify);
  return () => {
    window.removeEventListener("online", notify);
    window.removeEventListener("offline", notify);
  };
}

export function Courses({
  surface = "courses",
}: {
  surface?: "courses" | "learn_empty";
}) {
  const online = useSyncExternalStore(
    subscribeOnline,
    () => window.navigator.onLine,
    () => true,
  );
  const { t, number } = useUiPreferences();
  const streak = useStreak("courses");
  const wallet = useWallet("courses");
  const [search, setSearch] = useState("");
  const searchInput = useRef<HTMLInputElement>(null);
  const shell = useShellSession();
  const { data, isPending, isError, isFetching, dataUpdatedAt, refetch } =
    useQuery({
      queryKey: ["courses"],
      queryFn: () => api.courses(),
      retry: false,
    });
  const sessionActiveId = shell.courseId();
  const activeCourseId =
    sessionActiveId && data?.some((c) => c.id === sessionActiveId)
      ? sessionActiveId
      : data?.active_course_id || (data && data.length > 0 ? data[0].id : "");
  const filtered = data?.filter((course) =>
    `${course.title} ${course.description}`
      .toLocaleLowerCase()
      .includes(search.toLocaleLowerCase().trim()),
  );
  const clearSearch = () => {
    trackControl("clear_search");
    setSearch("");
    searchInput.current?.focus();
  };
  const create = (
    <AppLink
      className="duo-button primary"
      data-action-level="primary"
      href="/courses/create"
    >
      {t("courses.create")}
    </AppLink>
  );
  return (
    <section aria-labelledby="courses-heading" className="courses-page">
      <h1 id="courses-heading" className="courses-title">
        {t("nav.courses")}
      </h1>
      <div
        className="courses-toolbar"
        role="toolbar"
        aria-label={t("nav.courses")}
      >
        <div
          className="course-tabs"
          role="tablist"
          aria-label={t("courses.collections")}
        >
          <button
            className="course-tab selected"
            type="button"
            role="tab"
            aria-selected="true"
          >
            {t("courses.mine_tab")}
          </button>
          <button
            className="course-tab"
            type="button"
            role="tab"
            aria-selected="false"
            disabled
          >
            {t("courses.discover_tab")}
          </button>
        </div>
        <div className="courses-tools">
          <button
            type="button"
            className="courses-icon-button"
            aria-label={isError ? t("courses.retry") : t("courses.refresh")}
            aria-busy={(isFetching && !!data) || undefined}
            disabled={!online || isFetching}
            onClick={() => {
              trackControl(isError ? "retry_courses" : "refresh_courses");
              void refetch();
            }}
          >
            <StatIcon name="refresh" />
          </button>
          <Badge role="streak">
            <AppLink
              href="/streak"
              aria-label={`${t("nav.streak")} ${streak.data ? number(streak.data.current_days) : "-"}`}
            >
              <StatIcon name="flame" />
              {streak.data ? number(streak.data.current_days) : "-"}
            </AppLink>
          </Badge>
          <Badge role="credit">
            <AppLink
              href="/credits"
              aria-label={`${t("nav.credits")} ${wallet.data ? number(wallet.data.available) : "-"}`}
            >
              <StatIcon name="gem" />
              {wallet.data ? number(wallet.data.available) : "-"}
            </AppLink>
          </Badge>
          <span className="courses-create-bar">{create}</span>
        </div>
      </div>
      <input
        ref={searchInput}
        className="courses-search"
        type="search"
        aria-label={t("courses.search")}
        placeholder={t("courses.search")}
        value={search}
        onChange={(event) => setSearch(event.target.value)}
      />
      {!online && (
        <p role="status" className="feedback badge-warning">
          Offline
          {dataUpdatedAt > 0
            ? ` · as of ${new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(dataUpdatedAt)}`
            : ""}
          .{" "}
          {data
            ? "Showing the last loaded course list."
            : "Connect to load your courses."}
        </p>
      )}
      {isError && (
        <div className="feedback badge-danger" role="alert">
          <strong>Could not load courses.</strong>
          <p>
            Check your connection and try again.
            {data ? " Your last loaded courses are still shown." : ""}
          </p>
        </div>
      )}
      {search && filtered?.length !== 0 && (
        <Button variant="secondary" onClick={clearSearch}>
          Clear search
        </Button>
      )}
      {isPending && online && !isError && (
        <div aria-busy="true" className="course-loading">
          <p role="status">Loading your courses…</p>
          <div className="course-grid" aria-hidden="true">
            {[0, 1].map((item) => (
              <div className="course-skeleton" key={item}>
                <span className="skeleton-tile" />
                <span className="skeleton-line skeleton-line-title" />
                <span className="skeleton-line skeleton-line-meta" />
              </div>
            ))}
          </div>
        </div>
      )}
      {data?.length === 0 && (
        <h2 className="course-empty">What would you like to keep learning?</h2>
      )}
      {data && (
        <ExampleCourses
          surface={surface}
          enrolledSlugs={data.flatMap((c) =>
            c.example_slug ? [c.example_slug] : [],
          )}
        />
      )}
      {data && data.length > 0 && (
        <>
          <p role="status" className="courses-title">
            {filtered?.length} courses found
          </p>
          {filtered?.length === 0 && (
            <div className="course-empty card">
              <h2>No courses found</h2>
              <p>
                Try another search or clear your search to see your courses.
              </p>
              <Button variant="secondary" onClick={clearSearch}>
                Clear search
              </Button>
              <Button
                onClick={() => {
                  navigateTo("/courses/create");
                }}
              >
                Create a course
              </Button>
            </div>
          )}
          <div className="course-grid">
            {filtered?.map((course) => (
              <CourseCard
                key={course.id}
                course={course}
                current={course.id === activeCourseId}
                onRemove={async (id) => {
                  await api.deleteCourse(id);
                  await refetch();
                }}
              />
            ))}
          </div>
        </>
      )}
      <div className="courses-create-dock">{create}</div>
    </section>
  );
}
