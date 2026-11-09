import {
  createRootRoute,
  createRoute,
  createRouter,
  Link,
  Outlet,
  RouterProvider,
  useParams,
  useLocation,
} from "@tanstack/react-router";
import { LearnPath } from "./LearnPath";
import { LessonPlayer } from "./features/lesson-player/LessonPlayer";
import { Courses } from "./features/design/Courses";
import { PricingView } from "./features/pricing/PricingView";
import { SignIn } from "./features/accounts/SignIn";
import { RouteGuard } from "./features/accounts/RouteGuard";
import { AccountProfile } from "./features/accounts/AccountProfile";
function Layout() {
  const path = useLocation({ select: (location) => location.pathname });
  if (path === "/sign-in") return <Outlet />;
  return (
    <RouteGuard>
      <div className="shell">
        <a href="#main-content" className="skip-link">
          Skip to content
        </a>
        <aside className="sidebar">
          <Link className="brand" to="/">
            <span className="brand-mark" aria-hidden="true">
              <span />
              <span />
            </span>
            duolify
          </Link>
          <nav aria-label="Main">
            <Link to="/">◈ Learn</Link>
            <Link to="/courses">▦ Courses</Link>
            <Link to="/profile">◉ Profile</Link>
            <Link to="/credits">◇ Credits</Link>
          </nav>
          <p className="sidebar-foot">A little learning, every day.</p>
        </aside>
        <main className="main" id="main-content" tabIndex={-1}>
          <Outlet />
        </main>
        <aside className="right" aria-label="Learning tips">
          <div className="card">
            <h2>A little learning, every day</h2>
            <p>Choose a course and continue where you left off.</p>
          </div>
          <div className="card">
            <h2>Your learning, your pace</h2>
            <p>Each lesson is one small discovery.</p>
          </div>
        </aside>
      </div>
    </RouteGuard>
  );
}
const root = createRootRoute({ component: Layout });
const index = createRoute({
  getParentRoute: () => root,
  path: "/",
  component: () => <LearnPath courseId="astronomy" />,
});
const courses = createRoute({
  getParentRoute: () => root,
  path: "/courses",
  component: Courses,
});
function CoursePath() {
  const { courseId } = useParams({ from: "/course/$courseId" });
  return <LearnPath courseId={courseId} />;
}
const course = createRoute({
  getParentRoute: () => root,
  path: "/course/$courseId",
  component: CoursePath,
});
const lesson = createRoute({
  getParentRoute: () => root,
  path: "/lesson/$lessonId",
  component: LessonPlayer,
});
const profile = createRoute({
  getParentRoute: () => root,
  path: "/profile",
  component: AccountProfile,
});
const credits = createRoute({
  getParentRoute: () => root,
  path: "/credits",
  component: PricingView,
});
const signIn = createRoute({
  getParentRoute: () => root,
  path: "/sign-in",
  component: SignIn,
});
const router = createRouter({
  routeTree: root.addChildren([
    index,
    courses,
    course,
    lesson,
    profile,
    credits,
    signIn,
  ]),
});
declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
export default function App() {
  return <RouterProvider router={router} />;
}
