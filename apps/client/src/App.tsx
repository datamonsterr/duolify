import {
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
  useParams,
  type RouterHistory,
} from "@tanstack/react-router";
import { LearnPath } from "./LearnPath";
import { LessonPlayer } from "./features/lesson-player/LessonPlayer";
import { Courses } from "./features/design/Courses";
import { PricingView } from "./features/pricing/PricingView";
import { AppShell, MorePage } from "./features/shell/AppShell";
import {
  DestinationPage,
  RouteError,
  RoutePending,
  RouteUnavailable,
} from "./features/shell/RouteBoundary";
const root = createRootRoute({
  component: AppShell,
  pendingComponent: RoutePending,
  errorComponent: RouteError,
  notFoundComponent: RouteUnavailable,
});
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
function LessonRoute() {
  const { lessonId } = useParams({ from: "/lesson/$lessonId" });
  return <LessonPlayer key={lessonId} lessonId={lessonId} />;
}
const lesson = createRoute({
  getParentRoute: () => root,
  path: "/lesson/$lessonId",
  component: LessonRoute,
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
  component: PricingView,
});
const more = createRoute({
  getParentRoute: () => root,
  path: "/more",
  component: MorePage,
});
const friends = createRoute({
  getParentRoute: () => root,
  path: "/friends",
  component: () => <DestinationPage title="Friends" />,
});
const leagues = createRoute({
  getParentRoute: () => root,
  path: "/leagues",
  component: () => <DestinationPage title="Leagues" />,
});
const notes = createRoute({
  getParentRoute: () => root,
  path: "/notes",
  component: () => <DestinationPage title="Quick notes" />,
});
const bookmarks = createRoute({
  getParentRoute: () => root,
  path: "/bookmarks",
  component: () => <DestinationPage title="Bookmarks" />,
});
const settings = createRoute({
  getParentRoute: () => root,
  path: "/settings",
  component: () => <DestinationPage title="Settings" />,
});
const streak = createRoute({
  getParentRoute: () => root,
  path: "/streak",
  component: () => <DestinationPage title="Your streak" />,
});
const lessonTest = createRoute({
  getParentRoute: () => root,
  path: "/lesson/$lessonId/test",
  component: () => (
    <>
      <a className="shell-link" href="/course/astronomy">
        Exit test
      </a>
      <DestinationPage title="Lesson test" />
    </>
  ),
});
export const createAppRouter = (history?: RouterHistory) =>
  createRouter({
    history,
    defaultPendingComponent: RoutePending,
    defaultErrorComponent: RouteError,
    defaultNotFoundComponent: RouteUnavailable,
    notFoundMode: "root",
    routeTree: root.addChildren([
      index,
      courses,
      course,
      lesson,
      profile,
      credits,
      more,
      friends,
      leagues,
      notes,
      bookmarks,
      settings,
      streak,
      lessonTest,
    ]),
  });
const router = createAppRouter();
declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
export default function App() {
  return <RouterProvider router={router} />;
}
