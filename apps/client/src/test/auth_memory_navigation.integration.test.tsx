import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { afterEach, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { createAppRouter } from "../App";
import { credentialStore, session } from "../features/accounts/session";
import { server } from "./setup";

afterEach(async () => {
  await credentialStore.write(null);
  session.clear();
});

it("navigates after browser sign-in without discarding in-memory credentials", async () => {
  server.use(
    http.post("*/v1/auth/code", () => HttpResponse.json({}, { status: 202 })),
    http.post("*/v1/auth/verify", () =>
      HttpResponse.json({
        access_token: "access",
        refresh_token: "refresh",
        expires_in: 900,
      }),
    ),
    http.get("*/v1/me", () =>
      HttpResponse.json({
        id: "56d4e614-7665-4f14-b83e-8b5e9bed384a",
        display_name: "Learner",
        locale: "en",
        time_zone: "UTC",
        analytics_opt_out: false,
      }),
    ),
  );
  const router = createAppRouter(
    createMemoryHistory({ initialEntries: ["/sign-in"] }),
  );
  render(
    <QueryClientProvider client={new QueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  fireEvent.click(await screen.findByRole("button", { name: "Get started" }));
  fireEvent.change(screen.getByLabelText("Email address"), {
    target: { value: "browser@example.test" },
  });
  fireEvent.click(screen.getByLabelText("I am 16 or older"));
  fireEvent.click(screen.getByRole("button", { name: "Send code" }));
  fireEvent.change(await screen.findByLabelText("One-time code"), {
    target: { value: "123456" },
  });
  fireEvent.click(screen.getByRole("button", { name: /^Sign in$/ }));
  await waitFor(() => expect(router.state.location.pathname).toBe("/profile"));
  await expect(credentialStore.read()).resolves.toMatchObject({
    refresh_token: "refresh",
  });
  expect(sessionStorage.getItem("duolify.auth")).toBeNull();
});
