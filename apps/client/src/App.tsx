import {
  UiPreferencesProvider,
  useUiPreferences,
} from "./features/design/localization/Preferences";
import { resolveLocale } from "./features/design/localization/messages";
import { AuthError, session } from "./features/accounts/session";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  api,
  summarizeCourse,
  type Course,
  type CoursesResponse,
} from "./lib/api";
import { registerNavigator, registerPreloader } from "./lib/navigation";
import { useShellSession } from "./features/shell/ShellSession";
import {
  createRootRoute,
  lazyRouteComponent,
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
import { DailyCheckIn } from "./features/credits/DailyCheckIn";
import { SignIn } from "./features/accounts/SignIn";
import { RouteGuard } from "./features/accounts/RouteGuard";
import { courseTreeQuery, LearnPath } from "./LearnPath";
import { AppShell, MorePage } from "./features/shell/AppShell";
import { getDownloadedCourses } from "./features/player/library";
import {
  RouteError,
  RoutePending,
  RouteUnavailable,
} from "./features/shell/RouteBoundary";
import { AppLink } from "./lib/navigation";
// Route chunks load on navigation (or link hover); only sign-in and the Learn home are eager.
const ProgressPage = lazyRouteComponent(
  () => import("./features/gamification/DailyGoal"),
  "ProgressPage",
);
const LearningAnalyticsPage = lazyRouteComponent(
  () => import("./features/gamification/LearningAnalytics"),
  "LearningAnalyticsPage",
);
const LeaguePage = lazyRouteComponent(
  () => import("./features/gamification/League"),
  "LeaguePage",
);
const CourseBriefPage = lazyRouteComponent(
  () => import("./features/courses/CourseBrief"),
  "CourseBriefPage",
);
const Sessions = lazyRouteComponent(
  () => import("./features/accounts/Sessions"),
  "Sessions",
);
const SsoBridge = lazyRouteComponent(
  () => import("./features/accounts/SsoBridge"),
  "SsoBridge",
);
const SetupProfile = lazyRouteComponent(
  () => import("./features/accounts/LearnerProfile"),
  "SetupProfile",
);
const AccountProfile = lazyRouteComponent(
  () => import("./features/accounts/AccountProfile"),
  "AccountProfile",
);
const Settings = lazyRouteComponent(
  () => import("./features/accounts/Settings"),
  "Settings",
);
const FriendInvite = lazyRouteComponent(
  () => import("./features/social/FriendInvite"),
  "FriendInvite",
);
const FriendsPage = lazyRouteComponent(
  () => import("./features/social/FriendsPage"),
  "FriendsPage",
);
const CharacterLibrary = lazyRouteComponent(
  () => import("./features/characters/CharacterLibrary"),
  "CharacterLibrary",
);
const CourseCast = lazyRouteComponent(
  () => import("./features/characters/CourseCast"),
  "CourseCast",
);
const LessonPlayer = lazyRouteComponent(
  () => import("./features/lesson-player/LessonPlayer"),
  "LessonPlayer",
);
const LessonTestRoute = lazyRouteComponent(
  () => import("./features/lessontest/LessonTest"),
  "LessonTestRoute",
);
const Courses = lazyRouteComponent(
  () => import("./features/design/Courses"),
  "Courses",
);
const PricingView = lazyRouteComponent(
  () => import("./features/pricing/PricingView"),
  "PricingView",
);
const NotesPage = lazyRouteComponent(
  () => import("./features/notes/NotesPage"),
  "NotesPage",
);
const BookmarksPage = lazyRouteComponent(
  () => import("./features/notes/BookmarksPage"),
  "BookmarksPage",
);
const PublicPreviewPage = lazyRouteComponent(
  () => import("./features/sharing/PublicPreviewPage"),
  "PublicPreviewPage",
);
const MarketplacePage = lazyRouteComponent(
  () => import("./features/sharing/MarketplacePage"),
  "MarketplacePage",
);
const MarketCoursePage = lazyRouteComponent(
  () => import("./features/sharing/MarketCoursePage"),
  "MarketCoursePage",
);
const ModeratorQueue = lazyRouteComponent(
  () => import("./features/sharing/ModeratorQueue"),
  "ModeratorQueue",
);
const ModerationNotices = lazyRouteComponent(
  () => import("./features/sharing/ModerationNotices"),
  "ModerationNotices",
);
const BlockedCopy = lazyRouteComponent(
  () => import("./features/sharing/BlockedCopy"),
  "BlockedCopy",
);
// Wrapper routes borrow their page chunk's preload so the router waits for it, not Suspense.
function withPreload<T extends object>(
  wrapper: T,
  page: { preload?: () => Promise<void> | undefined },
): T {
  return Object.assign(wrapper, { preload: () => page.preload?.() });
}
function AccountShell() {
  return (
    <UiPreferencesProvider
      initialPreferences={{ locale: resolveLocale(session.current()?.locale) }}
    >
      <DailyCheckIn />
      <AppShell />
    </UiPreferencesProvider>
  );
}
function AuthenticatedShell() {
  const path = useLocation({ select: (location) => location.pathname });
  // Sign-in and the anonymous course link preview render without a session.
  if (
    path === "/sign-in" ||
    path === "/sso-bridge" ||
    /^\/c\/[A-Za-z0-9_-]+$/.test(path)
  )
    return <Outlet />;
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
  const { t } = useUiPreferences();
  const queryClient = useQueryClient();
  const { data: coursesData, isError: coursesFailed } = useQuery({
    queryKey: ["courses", owner],
    queryFn: async () => {
      try {
        const list = await api.courses({ authenticatedOnly: true });
        // Start the tree fetch now, not after this component re-renders with the list.
        const target = list.some((c) => c.id === shell.courseId())
          ? shell.courseId()
          : list.active_course_id || list[0]?.id;
        if (target)
          void queryClient.prefetchQuery(courseTreeQuery(target, owner));
        return list;
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
          const list = downloaded.map((d) =>
            summarizeCourse(d.courseData as Course),
          ) as CoursesResponse;
          list.active_course_id = downloaded[0].courseId;
          return list;
        }
        throw err;
      }
    },
    staleTime: 60000,
    // Offline with no downloaded course, or signed out: retrying cannot help.
    retry: (failures, err) =>
      failures < 3 &&
      !(err instanceof TypeError) &&
      !(err instanceof AuthError),
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
    // Course list failed with no offline copy: say so instead of loading forever.
    if (coursesFailed) return <p>{t("path.error")}</p>;
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
const marketplace = createRoute({
  getParentRoute: () => root,
  path: "/marketplace",
  component: MarketplacePage,
});
function MarketCourseRoute() {
  const { courseId } = useParams({ from: "/marketplace/$courseId" });
  return <MarketCoursePage id={courseId} />;
}
const marketplaceCourse = createRoute({
  getParentRoute: () => root,
  path: "/marketplace/$courseId",
  component: withPreload(MarketCourseRoute, MarketCoursePage),
});
const moderation = createRoute({
  getParentRoute: () => root,
  path: "/moderation",
  component: ModeratorQueue,
});
const moderationNotices = createRoute({
  getParentRoute: () => root,
  path: "/moderation/notices",
  component: ModerationNotices,
});
function BlockedCopyRoute() {
  const { courseId } = useParams({ from: "/moderation/blocked/$courseId" });
  return <BlockedCopy courseId={courseId} />;
}
const moderationBlocked = createRoute({
  getParentRoute: () => root,
  path: "/moderation/blocked/$courseId",
  component: withPreload(BlockedCopyRoute, BlockedCopy),
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
  const initialSlide =
    slide !== undefined && !isNaN(parseInt(slide, 10))
      ? parseInt(slide, 10)
      : undefined;
  return (
    <LessonPlayer
      key={`${lessonId}:${slide ?? ""}`}
      lessonId={lessonId}
      returnTo={returnTo}
      targetSlideId={slide}
      initialSlide={initialSlide}
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
    slide:
      typeof search.slide === "string"
        ? search.slide
        : typeof search.slide === "number"
          ? String(search.slide)
          : undefined,
  }),
  component: withPreload(LessonRoute, LessonPlayer),
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
      <AppLink href="/credits">Return to your wallet</AppLink>
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
  component: FriendsPage,
});
const friendInvite = createRoute({
  getParentRoute: () => root,
  path: "/friends/invite/$token",
  component: withPreload(function FriendInviteRoute() {
    const { token } = useParams({ from: "/friends/invite/$token" });
    return <FriendInvite token={token} />;
  }, FriendInvite),
});
const leagues = createRoute({
  getParentRoute: () => root,
  path: "/leagues",
  component: LeaguePage,
});
const notes = createRoute({
  getParentRoute: () => root,
  path: "/notes",
  component: NotesPage,
});
const bookmarks = createRoute({
  getParentRoute: () => root,
  path: "/bookmarks",
  component: BookmarksPage,
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
  component: withPreload(CourseCastRoute, CourseCast),
});
function CourseCastRoute() {
  const { courseId } = useParams({ from: "/courses/$courseId/cast" });
  return <CourseCast courseId={courseId} />;
}
const streak = createRoute({
  getParentRoute: () => root,
  path: "/streak",
  component: ProgressPage,
});
const analytics = createRoute({
  getParentRoute: () => root,
  path: "/analytics",
  component: LearningAnalyticsPage,
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
const ssoBridge = createRoute({
  getParentRoute: () => root,
  path: "/sso-bridge",
  component: SsoBridge,
});
const publicPreview = createRoute({
  getParentRoute: () => root,
  path: "/c/$token",
  component: withPreload(PublicPreviewRoute, PublicPreviewPage),
});
function PublicPreviewRoute() {
  const { token } = useParams({ from: "/c/$token" });
  return <PublicPreviewPage token={token} />;
}
function SignInRoute() {
  const navigate = useNavigate();
  return <SignIn onSignedIn={(path) => void navigate({ to: path })} />;
}
export const createAppRouter = (history?: RouterHistory) => {
  const router = buildRouter(history);
  // Same-app links and redirects navigate through the router (see lib/navigation).
  registerNavigator((to) => router.history.push(to));
  registerPreloader((to) => {
    // A failed warm-up is retried by the navigation itself.
    router.preloadRoute({ to } as never).catch(() => {});
  });
  return router;
};
const buildRouter = (history?: RouterHistory) =>
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
      marketplace,
      marketplaceCourse,
      moderation,
      moderationNotices,
      moderationBlocked,
      course,
      lesson,
      profile,
      profileSetup,
      credits,
      billing,
      more,
      friends,
      friendInvite,
      leagues,
      notes,
      bookmarks,
      settings,
      characterSettings,
      courseCast,
      sessions,
      streak,
      analytics,
      lessonTest,
      signIn,
      ssoBridge,
      publicPreview,
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
