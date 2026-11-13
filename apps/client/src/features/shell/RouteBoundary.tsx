import { Link, useRouter } from "@tanstack/react-router";
export function RoutePending() {
  return (
    <section aria-busy="true">
      <h1 tabIndex={-1}>Loading your page…</h1>
      <p role="status">Please wait.</p>
      <Link className="shell-link" to="/courses">
        Back to Courses
      </Link>
    </section>
  );
}
export function RouteUnavailable() {
  return (
    <section>
      <h1 tabIndex={-1}>This page isn't available</h1>
      <p>Try your courses or return to your learning path.</p>
      <div className="shell-links">
        <Link to="/courses">Back to Courses</Link>
        <Link to="/">Return to Learn</Link>
      </div>
    </section>
  );
}
export function RouteError({ reset }: { reset: () => void }) {
  const router = useRouter();
  return (
    <section>
      <h1 tabIndex={-1}>Couldn't open this page</h1>
      <p role="alert">Please try again. Your saved work is still available.</p>
      <button
        className="duo-button quiet"
        onClick={() => {
          void router.invalidate().then(reset);
        }}
      >
        Retry
      </button>
      <Link className="shell-link" to="/courses">
        Back to Courses
      </Link>
    </section>
  );
}
export function DestinationPage({ title }: { title: string }) {
  return (
    <section>
      <h1 tabIndex={-1}>{title}</h1>
      <p>This part of your learning space is not available yet.</p>
      <Link className="shell-link" to="/courses">
        Back to Courses
      </Link>
    </section>
  );
}
