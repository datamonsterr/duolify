import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  RouterProvider,
} from "@tanstack/react-router";
import { RouteError, RoutePending } from "./RouteBoundary";

function setup(importer: () => Promise<{ Page: () => React.ReactNode }>) {
  const root = createRootRoute({
    errorComponent: RouteError,
    pendingComponent: RoutePending,
  });
  const page = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: lazyRouteComponent(importer, "Page"),
  });
  const router = createRouter({
    history: createMemoryHistory({ initialEntries: ["/"] }),
    routeTree: root.addChildren([page]),
    defaultErrorComponent: RouteError,
    defaultPendingComponent: RoutePending,
  });
  render(<RouterProvider router={router} />);
}

describe("route chunk that fails to load", () => {
  it("offers Retry and opens the page once the chunk can load", async () => {
    let attempts = 0;
    setup(async () => {
      attempts += 1;
      if (attempts === 1) throw new TypeError("Failed to fetch dynamic import");
      return { Page: () => <h1>Lazy page</h1> };
    });
    expect(
      await screen.findByRole("heading", { name: "Couldn't open this page" }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(
      await screen.findByRole("heading", { name: "Lazy page" }),
    ).toBeInTheDocument();
    expect(attempts).toBe(2);
  });
});
