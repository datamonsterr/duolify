import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { server } from "./setup";
import { credentialStore, session } from "../features/accounts/session";
import { createAppRouter } from "../App";
import { quoteAPI } from "../features/pricing/quotes";
import { setLessonTestTracerEnabledForTest } from "../lib/features";

const courseId = "c1111111-1111-4111-8111-111111111111";
const lessonId = "l1111111-1111-4111-8111-111111111111";
const calls: string[] = [];
const quote = {
  id: "quote-1",
  pricing_version: "usd-quotes-test",
  action: "lesson_content",
  currency: "USD",
  content_ceiling: 137,
  audio_ceiling: 0,
  daily_free_eligible: false,
  expires_at: new Date(Date.now() + 600000).toISOString(),
  accepted: false,
};

beforeEach(async () => {
  calls.length = 0;
  setLessonTestTracerEnabledForTest(true);
  await credentialStore.write({
    access_token: "prep-access",
    refresh_token: "prep-refresh",
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
    http.get(`*/v1/courses/${courseId}`, () =>
      HttpResponse.json({
        id: courseId,
        title: "Everyday astronomy",
        description: "d",
        units: [
          {
            id: "u1",
            title: "Unit",
            lessons: [
              {
                id: lessonId,
                title: "Look up",
                goal: "g",
                state: "ready_to_generate",
              },
            ],
          },
        ],
      }),
    ),
  );
  vi.spyOn(quoteAPI, "create").mockImplementation(async () => {
    calls.push("quote");
    return { ...quote };
  });
});
afterEach(async () => {
  vi.restoreAllMocks();
  setLessonTestTracerEnabledForTest(null);
  await credentialStore.write(null);
  session.clear();
});

function mountPath() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <RouterProvider
        router={createAppRouter(
          createMemoryHistory({ initialEntries: [`/course/${courseId}`] }),
        )}
      />
    </QueryClientProvider>,
  );
}
const openPanel = async () => {
  fireEvent.click(
    await screen.findByRole("button", { name: "Prepare lesson" }),
  );
  return screen.findByRole("dialog");
};
function prepareReplies(status: number, body: object) {
  server.use(
    http.post(`*/v1/lessons/${lessonId}/prepare`, async ({ request }) => {
      calls.push(
        "prepare:" + ((await request.json()) as { quote_id: string }).quote_id,
      );
      return HttpResponse.json(body, { status });
    }),
  );
}

describe("LearnPath prepare lesson quote consent (#202)", () => {
  it("shows the API quote, accepts on confirm, then prepares in that order", async () => {
    vi.spyOn(quoteAPI, "accept").mockImplementation(async () => {
      calls.push("accept");
      return { accepted: true };
    });
    prepareReplies(202, { id: "gen-1", state: "generating" });
    mountPath();
    const dialog = await openPanel();
    expect(dialog).toHaveTextContent("Content ceiling: 137 credits");
    expect(calls).toEqual(["quote"]);
    fireEvent.click(screen.getByRole("button", { name: "Accept quote terms" }));
    await waitFor(() => expect(calls).toContain("prepare:quote-1"));
    expect(calls).toEqual(["quote", "accept", "prepare:quote-1"]);
  });

  it("declining leaves the lesson untouched: no accept, no prepare", async () => {
    const accept = vi.spyOn(quoteAPI, "accept");
    prepareReplies(202, { id: "gen-1" });
    mountPath();
    await openPanel();
    fireEvent.click(screen.getByRole("button", { name: "Not now" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(accept).not.toHaveBeenCalled();
    expect(calls).toEqual(["quote"]);
    expect(
      screen.getByRole("button", { name: "Prepare lesson" }),
    ).toBeEnabled();
  });

  it("surfaces a failed accept as an alert and never prepares", async () => {
    vi.spyOn(quoteAPI, "accept").mockRejectedValue(new Error("expired"));
    prepareReplies(202, { id: "gen-1" });
    mountPath();
    const dialog = await openPanel();
    fireEvent.click(screen.getByRole("button", { name: "Accept quote terms" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/renew quote/i);
    expect(dialog).toBeInTheDocument();
    expect(calls).toEqual(["quote"]);
  });

  it.each([
    [402, "insufficient_credits", /enough credits/i],
    [409, "quote_stale", /price changed or expired/i],
    [500, "boom", /something went wrong/i],
  ])(
    "shows a visible alert when prepare answers %i, without a silent refetch",
    async (status, error, text) => {
      vi.spyOn(quoteAPI, "accept").mockResolvedValue({ accepted: true });
      prepareReplies(status, { error });
      mountPath();
      await openPanel();
      fireEvent.click(
        screen.getByRole("button", { name: "Accept quote terms" }),
      );
      const alert = await screen.findByRole("alert");
      expect(alert).toHaveTextContent(text);
      expect(
        screen.getByRole("button", { name: /Prepare lesson|Try again/ }),
      ).toBeEnabled();
    },
  );
});
