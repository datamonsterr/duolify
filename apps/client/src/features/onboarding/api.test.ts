import { beforeEach, expect, it, vi } from "vitest";
import { session } from "../accounts/session";
import { acknowledge, loadProgress, listExamples } from "./api";
vi.mock("../accounts/session", async (load) => ({
  ...(await load<typeof import("../accounts/session")>()),
  session: { current: vi.fn(), request: vi.fn() },
}));
const owner = "11111111-1111-4111-8111-111111111111";
const key = `duolify.onboarding.v1:${owner}`;
beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  vi.mocked(session.current).mockReturnValue({ id: owner } as never);
});
it("keeps offline acknowledgements and flushes without learner text", async () => {
  vi.mocked(session.request).mockRejectedValue(new TypeError("offline"));
  acknowledge(owner, "welcome_state", "skipped");
  await vi.waitFor(() => expect(session.request).toHaveBeenCalled());
  expect(await loadProgress(owner)).toMatchObject({ welcome_state: "skipped" });
  vi.mocked(session.request).mockResolvedValue({
    welcome_state: "skipped",
    tour_state: "pending",
  });
  expect(await loadProgress(owner)).toMatchObject({
    welcome_state: "skipped",
    tour_state: "pending",
  });
  expect(session.request).toHaveBeenLastCalledWith(
    "/v1/me/onboarding",
    "PATCH",
    { welcome_state: "skipped" },
  );
  expect(JSON.parse(localStorage.getItem(key)!)).toMatchObject({ dirty: [] });
});
it("does not show a first-run prompt when there is no server or fallback evidence", async () => {
  const other = "22222222-2222-4222-8222-222222222222";
  vi.mocked(session.current).mockReturnValue({ id: other } as never);
  vi.mocked(session.request).mockRejectedValue(Error("offline"));
  expect(await loadProgress(other)).toBeNull();
});
it("hides absent examples without disabling the other paths", async () => {
  vi.mocked(session.request).mockRejectedValue(Error("404"));
  expect(await listExamples()).toEqual([]);
  vi.mocked(session.request).mockResolvedValue({ examples: [] });
  expect(await listExamples()).toEqual([]);
});
