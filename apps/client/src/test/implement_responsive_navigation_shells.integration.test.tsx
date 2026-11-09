import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { afterEach, expect, it, vi } from "vitest";
import { useState } from "react";
import { http, HttpResponse } from "msw";
import { server } from "./setup";
import { credentialStore, session } from "../features/accounts/session";
import { createAppRouter } from "../App";
import {
  ResponsivePanel,
  ListDetailLayout,
} from "../features/shell/AdaptiveLayouts";
import { configureUiAnalytics } from "../lib/analytics";
import { AppShell } from "../features/shell/AppShell";
import {
  RouteError,
  RoutePending,
  RouteUnavailable,
} from "../features/shell/RouteBoundary";

const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));

it("keeps pending/error recovery inside the shell and retries a failed loader without exposing its error", async () => {
  media(true);
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let fail = true;
  const root = createRootRoute({ component: AppShell });
  const page = createRoute({
    getParentRoute: () => root,
    path: "/settings",
    loader: async () => {
      await gate;
      if (fail) throw Error("private provider failure");
    },
    component: () => <h1>Settings ready</h1>,
  });
  const router = createRouter({
    routeTree: root.addChildren([page]),
    history: createMemoryHistory({ initialEntries: ["/settings"] }),
    defaultPendingMs: 0,
    defaultPendingMinMs: 0,
    defaultPendingComponent: RoutePending,
    defaultErrorComponent: RouteError,
    defaultNotFoundComponent: RouteUnavailable,
  });
  render(<RouterProvider router={router} />);
  expect(
    await screen.findByRole("heading", { name: "Loading your page…" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("navigation", { name: "Main" })).toBeInTheDocument();
  release();
  await screen.findByRole("heading", { name: "Couldn't open this page" });
  expect(
    screen.queryByText("private provider failure"),
  ).not.toBeInTheDocument();
  fail = false;
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(
    await screen.findByRole("heading", { name: "Settings ready" }),
  ).toBeInTheDocument();
});
vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));
afterEach(async () => {
  await credentialStore.write(null);
  session.clear();
  configureUiAnalytics(null);
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

export function media(initial = false) {
  let matches = initial;
  const listeners = new Set<() => void>();
  vi.stubGlobal("matchMedia", () => ({
    get matches() {
      return matches;
    },
    addEventListener: (_: string, fn: () => void) => listeners.add(fn),
    removeEventListener: (_: string, fn: () => void) => listeners.delete(fn),
  }));
  return (mobile: boolean) => {
    matches = mobile;
    listeners.forEach((fn) => fn());
  };
}

it("composes More, pricing and focused routes, restoring scroll and delivering only safe enums", async () => {
  media(true);
  await credentialStore.write({
    access_token: "shell-access",
    refresh_token: "shell-refresh",
    expires_in: 900,
  });
  server.use(
    http.get("*/v1/me", () =>
      HttpResponse.json({
        id: "c94d6abe-40d0-4d5a-8eb8-9e53c4fb9856",
        display_name: "Learner",
        locale: "en",
        time_zone: "UTC",
        background: "",
        analytics_opt_out: false,
      }),
    ),
  );
  const analyticsContext = {
    learnerId: "c94d6abe-40d0-4d5a-8eb8-9e53c4fb9856",
    enabled: true,
    platform: "web" as const,
    appVersion: "test",
  };
  const router = createAppRouter(
    createMemoryHistory({ initialEntries: ["/more"] }),
  );
  render(
    <QueryClientProvider client={new QueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  const heading = await screen.findByRole("heading", {
    name: "More",
  });
  await waitFor(() => expect(heading).toHaveFocus());
  configureUiAnalytics(analyticsContext);
  const main = screen.getByRole("main");
  main.scrollTop = 178;
  fireEvent.scroll(main);
  fireEvent.click(
    screen
      .getByRole("navigation", { name: "More destinations" })
      .querySelector('a[href="/credits"]')!,
  );
  await screen.findByRole("heading", { name: /credit/i });
  expect(
    screen
      .getByRole("navigation", { name: "Main" })
      .querySelector('a[href="/more"]'),
  ).toHaveAttribute("aria-current", "page");
  await router.navigate({ to: "/more" });
  await waitFor(() => expect(main.scrollTop).toBe(178));
  await router.navigate({
    to: "/lesson/$lessonId",
    params: { lessonId: "private-topic" },
  });
  await screen.findByTitle("Slide 1");
  expect(
    screen.queryByRole("navigation", { name: "Main" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Exit lesson" })).toHaveAttribute(
    "href",
    "/course/astronomy",
  );
  expect(posthog.capture).toHaveBeenCalledWith(
    "ui_navigation_selected",
    expect.objectContaining({
      destination: "credits",
      distinct_id: expect.stringMatching(/^[a-f0-9-]{36}$/),
      platform: "web",
      app_version: "test",
      source: "client",
    }),
  );
  expect(posthog.capture).toHaveBeenCalledWith(
    "ui_screen_viewed",
    expect.objectContaining({ screen_id: "lesson" }),
  );
  expect(JSON.stringify(posthog.capture.mock.calls)).not.toContain(
    "private-topic",
  );
});

it.each([
  "Course brief",
  "Outline chat",
  "Ask tutor",
  "Quick note",
  "Transcript",
])(
  "keeps one %s editor mounted from docked panel to modal, contains focus and restores its trigger",
  async (title) => {
    const resize = media();
    function Workspace() {
      const [open, setOpen] = useState(false);
      return (
        <>
          <button onClick={() => setOpen(true)}>Open brief</button>
          <ResponsivePanel
            title={title}
            open={open}
            onClose={() => setOpen(false)}
          >
            <label>
              Goal
              <textarea defaultValue="" />
            </label>
            <button>Confirm brief</button>
          </ResponsivePanel>
        </>
      );
    }
    const view = render(<Workspace />);
    const trigger = screen.getByRole("button", { name: "Open brief" });
    trigger.focus();
    fireEvent.click(trigger);
    const editor = screen.getByRole("textbox", { name: "Goal" });
    fireEvent.change(editor, { target: { value: "private draft" } });
    editor.focus();
    const { act } = await import("@testing-library/react");
    act(() => resize(true));
    const dialog = await screen.findByRole("dialog", { name: title });
    expect(screen.getByRole("textbox")).toBe(editor);
    expect(editor).toHaveFocus();
    expect(editor).toHaveValue("private draft");
    const confirm = screen.getByRole("button", { name: "Confirm brief" });
    confirm.focus();
    fireEvent.keyDown(dialog, { key: "Tab" });
    expect(
      screen.getByRole("button", { name: `Close ${title}` }),
    ).toHaveFocus();
    fireEvent.keyDown(dialog, { key: "Escape" });
    await waitFor(() => expect(trigger).toHaveFocus());
    fireEvent.click(trigger);
    expect(screen.getByRole("textbox")).toHaveValue("private draft");
    view.unmount();
    render(<Workspace />);
    fireEvent.click(screen.getByRole("button", { name: "Open brief" }));
    expect(screen.getByRole("textbox")).toHaveValue("");
  },
);

it("mobile step presentation moves focus, keeps its editor and restores the opener", async () => {
  media(true);
  function Workspace() {
    const [open, setOpen] = useState(false);
    return (
      <>
        <button onClick={() => setOpen(true)}>Review brief</button>
        <ResponsivePanel
          title="Confirm brief"
          mobilePresentation="step"
          open={open}
          onClose={() => setOpen(false)}
        >
          <textarea aria-label="Brief" defaultValue="Draft" />
        </ResponsivePanel>
      </>
    );
  }
  render(<Workspace />);
  const trigger = screen.getByRole("button", { name: "Review brief" });
  trigger.focus();
  fireEvent.click(trigger);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "Close Confirm brief" }),
  ).toHaveFocus();
  fireEvent.keyDown(screen.getByRole("region", { name: "Confirm brief" }), {
    key: "Escape",
  });
  await waitFor(() => expect(trigger).toHaveFocus());
});

it("maps a list/detail layout to one mobile surface while keeping both trees intact", async () => {
  media(true);
  const view = render(
    <ListDetailLayout
      list={<a href="/notes/n_42">Open note</a>}
      detail={<textarea aria-label="Edit note" defaultValue="draft" />}
      detailActive={false}
      back={<a href="/notes">Back to notes</a>}
    />,
  );
  expect(screen.getByRole("link", { name: "Open note" })).toBeVisible();
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  view.rerender(
    <ListDetailLayout
      list={<a href="/notes/n_42">Open note</a>}
      detail={<textarea aria-label="Edit note" defaultValue="draft" />}
      detailActive
      back={<a href="/notes">Back to notes</a>}
    />,
  );
  expect(screen.getByRole("textbox")).toHaveValue("draft");
  expect(screen.getByRole("link", { name: "Back to notes" })).toBeVisible();
});
