import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type ExampleSurface } from "../../lib/api";
import { trackExampleCoursesViewed } from "../../lib/analytics";
import { navigateTo } from "../../lib/navigation";
import { Badge } from "./Badge";
import { Button } from "./Button";

/**
 * "Start with an example": the authored, free example courses. Cards show only what the API
 * lists (title, description, size); enrolling makes a free copy and opens its learn path.
 */
export function ExampleCourses({
  surface,
  enrolledSlugs = [],
}: {
  surface: ExampleSurface;
  /** Slugs the learner already has; those cards are not offered again. */
  enrolledSlugs?: string[];
}) {
  const { data } = useQuery({
    queryKey: ["example-courses"],
    queryFn: () => api.exampleCourses(),
    retry: false,
    staleTime: 5 * 60_000,
  });
  const [pending, setPending] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const offered = (data ?? []).filter((e) => !enrolledSlugs.includes(e.slug));
  const shown = offered.length > 0;
  useEffect(() => {
    if (shown) trackExampleCoursesViewed(surface);
  }, [shown, surface]);
  if (!shown) return null;

  async function start(slug: string) {
    setPending(slug);
    setFailed(false);
    try {
      const { course_id } = await api.enrollExample(slug, surface);
      navigateTo(`/course/${encodeURIComponent(course_id)}`);
    } catch {
      setFailed(true);
      setPending(null);
    }
  }

  return (
    <section
      className="example-courses"
      aria-labelledby="example-courses-heading"
    >
      <h2 id="example-courses-heading">Start with an example</h2>
      <p className="muted">
        Free, ready-made courses. Try one before you create your own.
      </p>
      {failed && (
        <p role="alert" className="feedback badge-danger">
          Could not start the example. Check your connection and try again.
        </p>
      )}
      <div className="course-grid">
        {offered.map((example) => (
          <article
            key={example.slug}
            className="course-card bg-surface rounded-duo-lg"
            aria-labelledby={`example-${example.slug}`}
          >
            <div className="course-card-heading">
              <h3 id={`example-${example.slug}`}>
                <bdi>{example.title}</bdi>
              </h3>
            </div>
            <Badge role="info">Example</Badge>
            <p dir="auto">{example.description}</p>
            <span className="muted course-progress-copy">
              {example.lessons} lessons · about {example.estimated_minutes} min
              · Free
            </span>
            <Button
              pending={pending === example.slug}
              disabled={pending !== null}
              aria-label={`Start ${example.title}`}
              onClick={() => void start(example.slug)}
            >
              Start
            </Button>
          </article>
        ))}
      </div>
    </section>
  );
}
