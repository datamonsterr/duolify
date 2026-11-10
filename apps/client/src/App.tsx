import { UiPreferencesProvider } from "./features/design/localization/Preferences";
import { resolveLocale } from "./features/design/localization/messages";
import { AuthError, session } from "./features/accounts/session";
import { useQuery } from "@tanstack/react-query";
import { api, type Course, type CoursesResponse } from "./lib/api";
import { useShellSession } from "./features/shell/ShellSession";
import {
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
  Outlet,
  useLocation,
  useNavigate,
  useParams,
  useSearch,
  type RouterHistory,
} from "@tanstack/react-router";
import { CourseBriefPage } from "./features/courses/CourseBrief";
import { Sessions } from "./features/accounts/Sessions";
import { SignIn } from "./features/accounts/SignIn";
import { RouteGuard } from "./features/accounts/RouteGuard";
import { SetupProfile } from "./features/accounts/LearnerProfile";
import { AccountProfile } from "./features/accounts/AccountProfile";
import { Settings } from "./features/accounts/Settings";
import { CharacterLibrary } from "./features/characters/CharacterLibrary";
import { CourseCast } from "./features/characters/CourseCast";
import { LearnPath } from "./LearnPath";
import { LessonPlayer } from "./features/lesson-player/LessonPlayer";
import { LessonTestRoute } from "./features/lessontest/LessonTest";
import { Courses } from "./features/design/Courses";
import { PricingView } from "./features/pricing/PricingView";
import { AppShell, MorePage } from "./features/shell/AppShell";
import { getDownloadedCourses } from "./features/player/library";
import {
  DestinationPage,
  RouteError,
  RoutePending,
  RouteUnavailable,
} from "./features/shell/RouteBoundary";
function AccountShell() {
  return (
    <UiPreferencesProvider
      initialPreferences={{ locale: resolveLocale(session.current()?.locale) }}
    >
      <AppShell />
    </UiPreferencesProvider>
  );
}
function AuthenticatedShell() {
  const path = useLocation({ select: (location) => location.pathname });
  if (path === "/sign-in") return <Outlet />;
  return (
    <RouteGuard path={path}>
      <AccountShell />
    </RouteGuard>
  );
}
const root = createRootRoute({
  component: AuthenticatedShell,
  pendingComponent: RoutePending,
  errorComponent: RouteError,
  notFoundComponent: RouteUnavailable,
});
function IndexRoute() {
  const shell = useShellSession();
  const owner = session.current()?.id ?? "";
  const { data: coursesData } = useQuery({
    queryKey: ["courses", owner],
    queryFn: async () => {
      try {
        return await api.courses({ authenticatedOnly: true });
      } catch (err) {
        if (
          err instanceof AuthError ||
          !(err instanceof TypeError || !navigator.onLine)
        )
          throw err;
        const downloaded = Object.values(getDownloadedCourses(owner)).filter(
          (d) => d.courseData,
        );
        if (downloaded.length > 0 && downloaded[0].courseData) {
          const list = downloaded.map(
            (d) => d.courseData as Course,
          ) as CoursesResponse;
          list.active_course_id = downloaded[0].courseId;
          return list;
        }
        throw err;
      }
    },
    staleTime: 60000,
  });

  const activeId = shell.courseId();
  let resolvedCourseId = activeId;

  if (coursesData && coursesData.length > 0) {
    const exists = coursesData.some((c) => c.id === activeId);
    if (!exists) {
      resolvedCourseId = coursesData.active_course_id || coursesData[0].id;
    }
  } else if (!resolvedCourseId && coursesData?.active_course_id) {
    resolvedCourseId = coursesData.active_course_id;
  }

  if (resolvedCourseId && resolvedCourseId !== activeId) {
    shell.rememberCourse(resolvedCourseId, false);
  }

  if (!resolvedCourseId) {
    if (coursesData && coursesData.length === 0) {
      return <Courses />;
    }
    return <p role="status">Loading course…</p>;
  }

  return <LearnPath courseId={resolvedCourseId} />;
}

const index = createRoute({
  getParentRoute: () => root,
  path: "/",
  component: IndexRoute,
});
const courses = createRoute({
  getParentRoute: () => root,
  path: "/courses",
  component: Courses,
});
const createCourse = createRoute({
  getParentRoute: () => root,
  path: "/courses/create",
  component: CourseBriefPage,
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
  const { returnTo, slide } = useSearch({ from: "/lesson/$lessonId" });
  return (
    <LessonPlayer
      key={`${lessonId}:${slide ?? ""}`}
      lessonId={lessonId}
      returnTo={returnTo}
      targetSlideId={slide}
    />
  );
}
const lesson = createRoute({
  getParentRoute: () => root,
  path: "/lesson/$lessonId",
  validateSearch: (
    search: Record<string, unknown>,
  ): { returnTo?: string; slide?: string } => ({
    returnTo: typeof search.returnTo === "string" ? search.returnTo : undefined,
    slide: typeof search.slide === "string" ? search.slide : undefined,
  }),
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
const billing = createRoute({
  getParentRoute: () => root,
  path: "/credits/billing",
  component: () => (
    <section className="pricing">
      <h1>Manage billing with Stripe</h1>
      <p>
        Test billing only. No real subscription, payment, cancellation or plan
        change is made.
      </p>
      <p>
        Live Stripe billing management becomes available after payment setup and
        approval. Issued credits remain available after cancellation. Plan
        changes apply at renewal.
      </p>
      <a href="/credits">Return to your wallet</a>
    </section>
  ),
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
  component: Settings,
});
const sessions = createRoute({
  getParentRoute: () => root,
  path: "/settings/sessions",
  component: Sessions,
});
const characterSettings = createRoute({
  getParentRoute: () => root,
  path: "/settings/characters",
  component: CharacterLibrary,
});
const courseCast = createRoute({
  getParentRoute: () => root,
  path: "/courses/$courseId/cast",
  component: CourseCastRoute,
});
function CourseCastRoute() {
  const { courseId } = useParams({ from: "/courses/$courseId/cast" });
  return <CourseCast courseId={courseId} />;
}
const streak = createRoute({
  getParentRoute: () => root,
  path: "/streak",
  component: () => <DestinationPage title="Your streak" />,
});
const lessonTest = createRoute({
  getParentRoute: () => root,
  path: "/lesson/$lessonId/test",
  component: LessonTestRoute,
});
const signIn = createRoute({
  getParentRoute: () => root,
  path: "/sign-in",
  component: SignInRoute,
});
function SignInRoute() {
  const navigate = useNavigate();
  return <SignIn onSignedIn={(path) => void navigate({ to: path })} />;
}
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
      createCourse,
      course,
      lesson,
      profile,
      profileSetup,
      credits,
      billing,
      more,
      friends,
      leagues,
      notes,
      bookmarks,
      settings,
      characterSettings,
      courseCast,
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
