import { StrictMode } from "react";
import { render, screen, waitFor, cleanup } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { DailyCheckIn } from "./DailyCheckIn";

const { request } = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("../accounts/session", () => ({
  session: {
    current: () => ({ id: "attendance-learner" }),
    expired: () => false,
    snapshot: () => 1,
    subscribe: () => () => {},
    request,
  },
}));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  request.mockReset();
});

it("shares the in-flight check-in across React effect replay and announces once", async () => {
  request.mockResolvedValue({
    granted: 400,
    first_time: true,
    checked_in: true,
    balance: 446,
    banked_checkin: 400,
    cap: 1050,
    next_check_in_at: new Date(Date.now() + 86400000).toISOString(),
  });
  render(
    <StrictMode>
      <QueryClientProvider client={new QueryClient()}>
        <DailyCheckIn />
      </QueryClientProvider>
    </StrictMode>,
  );
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent("+400"),
  );
  expect(request).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("status")).toHaveTextContent("446");
});
