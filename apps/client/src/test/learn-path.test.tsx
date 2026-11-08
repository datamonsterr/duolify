import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { LearnPath } from "../LearnPath";
import { api } from "../lib/api";

describe("learn path", () => {
  it("loads the course from the API and shows lesson states", async () => {
    expect((await api.courses())[0].title).toBe("Everyday astronomy");
    render(
      <QueryClientProvider client={new QueryClient()}>
        <LearnPath courseId="astronomy" />
      </QueryClientProvider>,
    );
    expect(await screen.findByText("Look up")).toBeTruthy();
    expect(screen.getAllByText("Locked")[0]).toBeTruthy();
  });
});
