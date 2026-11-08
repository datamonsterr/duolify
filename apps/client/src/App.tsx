import {
  createRootRoute,
  createRoute,
  createRouter,
  Link,
  Outlet,
  RouterProvider,
  useParams,
} from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "./lib/api";
import { LearnPath } from "./LearnPath";
import { LessonPlayer } from "./features/lesson-player/LessonPlayer";
function Layout() {
  return (
    <div className="shell">
      <aside className="sidebar">
        <Link className="brand" to="/">
          ✦ duolify
        </Link>
        <nav>
          <Link to="/">◈ Learn</Link>
          <Link to="/courses">▦ Courses</Link>
          <Link to="/profile">◉ Profile</Link>
          <Link to="/credits">◇ Credits</Link>
        </nav>
        <p className="sidebar-foot">A little learning, every day.</p>
      </aside>
      <main className="main">
        <Outlet />
      </main>
      <aside className="right">
        <div className="stats">
          <span>
            🔥 7 <small>day streak</small>
          </span>
          <span>
            ✦ 240 <small>XP</small>
          </span>
          <span>
            ♥ 5 <small>hearts</small>
          </span>
        </div>
        <div className="card">
          <div className="eyebrow">DAILY QUEST</div>
          <h3>Make space to learn</h3>
          <p>Complete one lesson today.</p>
          <div className="quest">
            <span />
          </div>
          <small>0 / 1 lessons</small>
        </div>
        <div className="card">
          <h3>Keep your curiosity going</h3>
          <p>A few minutes a day adds up to something wonderful.</p>
        </div>
      </aside>
    </div>
  );
}
function Courses() {
  const { data, isPending, isError } = useQuery({
    queryKey: ["courses"],
    queryFn: api.courses,
  });
  return (
    <>
      <div className="eyebrow">EXPLORE SOMETHING NEW</div>
      <h1>Courses</h1>
      <p>What would you like to learn?</p>
      {isPending && <p>Loading courses…</p>}
      {isError && <p>Could not load courses.</p>}
      <div className="course-grid">
        {data?.map((c) => (
          <Link
            className="course-card"
            key={c.id}
            to="/course/$courseId"
            params={{ courseId: c.id }}
          >
            <span className="course-icon">
              {c.id === "astronomy" ? "✦" : "❋"}
            </span>
            <h2>{c.title}</h2>
            <p>{c.description}</p>
            <strong>Explore course →</strong>
          </Link>
        ))}
      </div>
    </>
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
  component: () => (
    <>
      <div className="eyebrow">YOUR SPACE</div>
      <h1>Profile</h1>
      <p>Your learning journey starts here.</p>
    </>
  ),
});
const credits = createRoute({
  getParentRoute: () => root,
  path: "/credits",
  component: () => (
    <>
      <div className="eyebrow">YOUR BALANCE</div>
      <h1>Credits</h1>
      <p>Credits will power your future learning adventures.</p>
    </>
  ),
});
const router = createRouter({
  routeTree: root.addChildren([
    index,
    courses,
    course,
    lesson,
    profile,
    credits,
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
