import { StrictMode } from "react";
import { render, screen, waitFor, cleanup, act } from "@testing-library/react";
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
  vi.useRealTimers();
  request.mockReset();
});

it("an offline open waits for connectivity and grants only the current attendance", async () => {
  let online = false;
  vi.spyOn(navigator, "onLine", "get").mockImplementation(() => online);
  request.mockResolvedValue({
    granted: 150,
    checked_in: true,
    first_time: false,
    balance: 550,
    banked_checkin: 550,
    cap: 1050,
    next_check_in_at: new Date(Date.now() + 86400000).toISOString(),
  });
  render(
    <QueryClientProvider client={new QueryClient()}>
      <DailyCheckIn />
    </QueryClientProvider>,
  );
  expect(request).not.toHaveBeenCalled();
  online = true;
  await act(async () => window.dispatchEvent(new Event("online")));
  expect(screen.getByRole("status")).toHaveTextContent("+150");
  expect(request).toHaveBeenCalledTimes(1);
});

it("checks the server day boundary while the app remains open", async () => {
  vi.useFakeTimers();
  const now = Date.now();
  request
    .mockResolvedValueOnce({
      granted: 400,
      checked_in: true,
      first_time: true,
      balance: 400,
      banked_checkin: 400,
      cap: 1050,
      next_check_in_at: new Date(now + 1000).toISOString(),
    })
    .mockResolvedValueOnce({
      granted: 150,
      checked_in: true,
      first_time: false,
      balance: 550,
      banked_checkin: 550,
      cap: 1050,
      next_check_in_at: new Date(now + 86400000).toISOString(),
    });
  await act(async () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <DailyCheckIn />
      </QueryClientProvider>,
    );
  });
  expect(screen.getByRole("status")).toHaveTextContent("+400");
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1001);
  });
  expect(screen.getByRole("status")).toHaveTextContent("+150");
  expect(request).toHaveBeenCalledTimes(2);
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
