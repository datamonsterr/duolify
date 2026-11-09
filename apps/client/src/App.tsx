import {
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
  Outlet,
  useLocation,
  useParams,
  type RouterHistory,
} from "@tanstack/react-router";
import { Sessions } from "./features/accounts/Sessions";
import { SignIn } from "./features/accounts/SignIn";
import { RouteGuard } from "./features/accounts/RouteGuard";
import { SetupProfile } from "./features/accounts/LearnerProfile";
import { AccountProfile } from "./features/accounts/AccountProfile";
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
function AuthenticatedShell() {
  const path = useLocation({ select: (location) => location.pathname });
  if (path === "/sign-in") return <Outlet />;
  return (
    <RouteGuard path={path}>
      <AppShell />
    </RouteGuard>
  );
}
const root = createRootRoute({
  component: AuthenticatedShell,
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
  component: AccountProfile,
});
const profileSetup = createRoute({
  getParentRoute: () => root,
  path: "/profile/setup",
  component: SetupProfile,
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
  component: () => (
    <>
      <DestinationPage title="Settings" />
      <a className="shell-link" href="/settings/sessions">
        Manage sessions
      </a>
    </>
  ),
});
const sessions = createRoute({
  getParentRoute: () => root,
  path: "/settings/sessions",
  component: Sessions,
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
const signIn = createRoute({
  getParentRoute: () => root,
  path: "/sign-in",
  component: SignIn,
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
      profileSetup,
      credits,
      more,
      friends,
      leagues,
      notes,
      bookmarks,
      settings,
      sessions,
      streak,
      lessonTest,
      signIn,
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
