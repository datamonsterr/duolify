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
  return (
    <section aria-labelledby="courses-heading" className="courses-page">
      <header className="courses-header">
        <div>
          <h1 id="courses-heading">Courses</h1>
        </div>
        <label className="search-label courses-search">
          Search courses
          <input
            ref={searchInput}
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </label>
        <AppLink
          className="duo-button primary courses-create"
          data-action-level="primary"
          href="/courses/create"
        >
          Create a course
        </AppLink>
      </header>
      <div className="course-toolbar">
        <div
          className="course-tabs"
          role="tablist"
          aria-label="Course collections"
        >
          <button
            className="course-tab selected"
            type="button"
            role="tab"
            aria-selected="true"
          >
            My courses
          </button>
          <button
            className="course-tab"
            type="button"
            role="tab"
            aria-selected="false"
            disabled
          >
            Discover
          </button>
        </div>
      </div>
      <div className="course-context">
        <Badge role="streak">
          <span aria-hidden="true" className="streak-icon">
            ♨
          </span>{" "}
          Streaks grow with daily learning
        </Badge>
        <Badge role="credit">
          <AppLink href="/credits">
            <span aria-hidden="true" className="credit-icon">
              ◇
            </span>{" "}
            View credits
          </AppLink>
        </Badge>
      </div>
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
      <div className="course-actions">
        <Button
          variant={isError ? "secondary" : "quiet"}
          pending={isFetching && !!data}
          disabled={!online || (isFetching && !data)}
          onClick={() => {
            trackControl(isError ? "retry_courses" : "refresh_courses");
            void refetch();
          }}
        >
          {isError ? "Retry" : "Refresh courses"}
        </Button>
        {search && filtered?.length !== 0 && (
          <Button variant="secondary" onClick={clearSearch}>
            Clear search
          </Button>
        )}
      </div>
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
        <div className="course-empty card">
          <div className="brand-mark" aria-hidden="true">
            <span />
            <span />
          </div>
          <h2>What would you like to keep learning?</h2>
          <Button
            variant="secondary"
            onClick={() => {
              navigateTo("/courses/create");
            }}
          >
            Create a course
          </Button>
        </div>
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
          <p role="status" className="muted">
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
    </section>
  );
}
