import { render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { expect, it } from "vitest";
import { server } from "../../test/setup";
import { Courses } from "./Courses";

it("groups the actions in one named toolbar and labels search for screen readers only", async () => {
  server.use(
    http.get("*/courses", () => HttpResponse.json([])),
    http.get("*/v1/me/credits", () => HttpResponse.json({})),
  );
  render(
    <QueryClientProvider client={new QueryClient()}>
      <Courses />
    </QueryClientProvider>,
  );
  const toolbar = screen.getByRole("toolbar", { name: "Courses" });
  expect(within(toolbar).getAllByRole("tab")).toHaveLength(2);
  expect(
    within(toolbar).getByRole("button", { name: "Refresh courses" }),
  ).toBeInTheDocument();
  expect(
    within(toolbar).getByRole("link", { name: /Credits/ }),
  ).toHaveAttribute("href", "/credits");
  expect(within(toolbar).getByRole("link", { name: /streak/i })).toBeVisible();
  const search = screen.getByRole("searchbox", { name: "Search courses" });
  expect(search).toHaveAttribute("placeholder", "Search courses");
  expect(search.closest("label")).toBeNull();
  expect(
    await screen.findByRole("heading", {
      name: "What would you like to keep learning?",
    }),
  ).toBeVisible();
});
