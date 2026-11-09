import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "./setup";
import { credentialStore, session } from "../features/accounts/session";

afterEach(async () => {
  await credentialStore.write(null);
  session.clear();
});
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { createAppRouter } from "../App";
import { api } from "../lib/api";

describe("learn path", () => {
  it("loads the course from the API and shows lesson states", async () => {
    await credentialStore.write({
      access_token: "learn-path-access",
      refresh_token: "learn-path-refresh",
      expires_in: 900,
    });
    server.use(
      http.get("*/v1/me", () =>
        HttpResponse.json({
          id: "56d4e614-7665-4f14-b83e-8b5e9bed384a",
          display_name: "Learner",
          locale: "en",
          time_zone: "UTC",
          background: "",
          analytics_opt_out: true,
        }),
      ),
    );
    expect((await api.courses())[0].title).toBe("Everyday astronomy");
    render(
      <QueryClientProvider client={new QueryClient()}>
        <RouterProvider
          router={createAppRouter(
            createMemoryHistory({ initialEntries: ["/course/astronomy"] }),
          )}
        />
      </QueryClientProvider>,
    );
    expect(await screen.findByText("Look up")).toBeTruthy();
    expect(screen.getAllByText("Locked")[0]).toBeTruthy();
  });
});
