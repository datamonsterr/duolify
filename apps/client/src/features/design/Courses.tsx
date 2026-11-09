import { useRef, useState, useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../../lib/api";
import { trackControl } from "../../lib/analytics";
import { Badge } from "./Badge";
import { Button } from "./Button";
import { CourseCard } from "./CourseCard";

function subscribeOnline(notify: () => void) {
  window.addEventListener("online", notify);
  window.addEventListener("offline", notify);
  return () => {
    window.removeEventListener("online", notify);
    window.removeEventListener("offline", notify);
  };
}

export function Courses() {
  const online = useSyncExternalStore(
    subscribeOnline,
    () => window.navigator.onLine,
    () => true,
  );
  const [search, setSearch] = useState("");
  const searchInput = useRef<HTMLInputElement>(null);
  const { data, isPending, isError, isFetching, dataUpdatedAt, refetch } =
    useQuery({ queryKey: ["courses"], queryFn: api.courses, retry: false });
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
    <section aria-labelledby="courses-heading">
      <header className="courses-header">
        <div>
          <div className="eyebrow">Keep your curiosity going</div>
          <h1 id="courses-heading">Courses</h1>
        </div>
        <Button disabled aria-describedby="creation-unavailable">
          Create a course
        </Button>
      </header>
      <p id="creation-unavailable" className="muted">
        Course creation is not available yet.
      </p>
      <div className="course-toolbar">
        <div className="course-tabs" aria-label="Course collections">
          <span className="badge badge-info">My courses</span>
          <Button
            variant="quiet"
            disabled
            aria-describedby="discover-unavailable"
          >
            Discover
          </Button>
        </div>
        <p id="discover-unavailable" className="muted">
          Public course discovery is not available yet.
        </p>
        <label className="search-label">
          Search courses
          <input
            ref={searchInput}
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </label>
      </div>
      <div className="course-context">
        <Badge role="streak">
          <span aria-hidden="true" className="streak-icon">
            ♨
          </span>{" "}
          Streaks grow with daily learning
        </Badge>
        <Badge role="credit">
          <a href="/credits">
            <span aria-hidden="true" className="credit-icon">
              ◇
            </span>{" "}
            View credits
          </a>
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
          variant={isError ? "secondary" : "primary"}
          pending={isFetching && !!data}
          disabled={!online || (isFetching && !data)}
          onClick={() => {
            trackControl(isError ? "retry_courses" : "refresh_courses");
            void refetch();
          }}
        >
          {isError ? "Retry" : "Refresh courses"}
        </Button>
        {search && (
          <Button variant="secondary" onClick={clearSearch}>
            Clear search
          </Button>
        )}
      </div>
      {isPending && online && !isError && (
        <div aria-busy="true" className="course-loading">
          <p role="status">Loading your courses…</p>
          <div className="course-grid" aria-hidden="true">
            <div className="course-skeleton" />
            <div className="course-skeleton" />
          </div>
        </div>
      )}
      {data?.length === 0 && (
        <div className="course-empty">
          <div className="brand-mark" aria-hidden="true">
            <span />
            <span />
          </div>
          <h2>What would you like to keep learning?</h2>
          <p>
            Start with a topic. You'll see a preview before anything is created.
          </p>
          <Button disabled aria-describedby="creation-unavailable">
            Create a course
          </Button>
        </div>
      )}
      {data && data.length > 0 && (
        <>
          <p role="status" className="muted">
            {filtered?.length} courses found
          </p>
          {filtered?.length === 0 && (
            <div className="course-empty">
              <h2>No courses found</h2>
              <p>
                Try another search or clear your search to see your courses.
              </p>
            </div>
          )}
          <div className="course-grid">
            {filtered?.map((course) => (
              <CourseCard
                key={course.id}
                course={course}
                current={course.id === "astronomy"}
              />
            ))}
          </div>
        </>
      )}
    </section>
  );
}
