import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MissingLessons } from "./MissingLessons";
import { missingFailure, missingMessages } from "./missing";
import { session } from "../accounts/session";

vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));
const request = vi.mocked(session.request);

const status = (over: Record<string, unknown> = {}) => ({
  course_id: "c1",
  version: 3,
  total: 3,
  ready: 2,
  missing: 1,
  state: "ready",
  next_lesson_id: "l3",
  ...over,
});
const quote = (over: Record<string, unknown> = {}) => ({
  quote_id: "q1",
  lesson_id: "l3",
  pricing_version: "2026.11",
  content_credits: 140,
  audio_credits: 50,
  total_credits: 190,
  balance: 400,
  sufficient: true,
  expires_at: "2026-11-15T10:00:00Z",
  ...over,
});
const rejected = (httpStatus: number, code = "server text") =>
  Object.assign(new Error(code), { status: httpStatus });

/** Routes each request to a canned answer so call order never matters. */
function api(routes: Record<string, () => unknown>) {
  request.mockImplementation(async (path: string, method = "GET") => {
    const handler = routes[`${method} ${path}`];
    if (!handler) throw new Error(`unexpected ${method} ${path}`);
    const value = handler();
    if (value instanceof Error) throw value;
    return value as never;
  });
}
const base = "/v1/courses/c1/missing-lessons";
const view = () => render(<MissingLessons courseId="c1" pollMs={5} />);

// A block body: vitest would call a returned mock as the cleanup hook.
beforeEach(() => {
  request.mockReset();
});

describe("failure classification", () => {
  it("uses status and closed codes only, never server text", () => {
    expect(missingFailure(rejected(402, "insufficient_credits"))).toBe(
      "insufficient_credits",
    );
    expect(missingFailure(rejected(409, "quote_stale"))).toBe("stale_quote");
    expect(missingFailure(rejected(409, "previous_lesson_not_passed"))).toBe(
      "locked",
    );
    expect(missingFailure(rejected(409, "generation_in_progress"))).toBe(
      "in_progress",
    );
    expect(missingFailure(rejected(404))).toBe("unavailable");
    expect(missingFailure(rejected(503))).toBe("failed");
    expect(missingFailure(new TypeError("Failed to fetch"))).toBe("offline");
    for (const text of Object.values(missingMessages))
      expect(text).not.toContain("server text");
  });
});

describe("missing lessons panel", () => {
  it("shows ready over total, the free-versus-paid note and the quote button", async () => {
    api({ [`GET ${base}`]: () => status() });
    view();
    expect(await screen.findByTestId("missing-ready")).toHaveTextContent(
      "2 of 3 lessons ready",
    );
    expect(
      screen.getByText(/Ready lessons are free for you/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Review quote" }),
    ).toBeInTheDocument();
  });

  it("waits for the previous lesson test and offers no quote", async () => {
    api({ [`GET ${base}`]: () => status({ state: "locked" }) });
    view();
    expect(
      await screen.findByText(/Pass the previous lesson test/),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Review quote" })).toBeNull();
  });

  it("renders nothing to buy once every lesson is ready", async () => {
    api({
      [`GET ${base}`]: () =>
        status({ state: "complete", ready: 3, missing: 0 }),
    });
    view();
    expect(await screen.findByTestId("missing-ready")).toHaveTextContent(
      "3 of 3 lessons ready",
    );
    expect(screen.queryByRole("button", { name: "Review quote" })).toBeNull();
  });

  it("requires explicit consent to the shown price version before generating", async () => {
    let polls = 0;
    api({
      [`GET ${base}`]: () => status(),
      [`POST ${base}/quote`]: () => quote(),
      [`POST ${base}/generate`]: () => ({
        generation_id: "g1",
        state: "queued",
      }),
      "GET /v1/lesson-generations/g1": () => ({
        state: ++polls > 1 ? "completed" : "generating",
      }),
    });
    view();
    fireEvent.click(
      await screen.findByRole("button", { name: "Review quote" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Generate next lesson",
    });
    expect(screen.getByTestId("quote-version")).toHaveTextContent(
      "Price version 2026.11",
    );
    expect(screen.getByTestId("quote-content")).toHaveTextContent(
      "140 credits",
    );
    expect(screen.getByTestId("quote-audio")).toHaveTextContent("50 credits");
    expect(screen.getByTestId("quote-total")).toHaveTextContent(
      "up to 190 credits",
    );
    const generate = screen.getByRole("button", { name: "Generate lesson" });
    expect(generate).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /I agree to spend up to 190/ }),
    );
    expect(generate).toBeEnabled();
    api({
      [`GET ${base}`]: () =>
        status({ state: "complete", ready: 3, missing: 0 }),
      [`POST ${base}/generate`]: () => ({
        generation_id: "g1",
        state: "queued",
      }),
      "GET /v1/lesson-generations/g1": () => ({
        state: ++polls > 1 ? "completed" : "generating",
      }),
    });
    fireEvent.click(generate);
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(`${base}/generate`, "POST", {
        lesson_id: "l3",
        quote_id: "q1",
        consent: true,
      }),
    );
    expect(await screen.findByTestId("missing-status")).toHaveTextContent(
      "Lesson ready",
    );
    expect(screen.getByTestId("missing-ready")).toHaveTextContent(
      "3 of 3 lessons ready",
    );
    expect(dialog).not.toBeInTheDocument();
  });

  it("explains a shortfall, links to credit options and never enables Generate", async () => {
    api({
      [`GET ${base}`]: () => status(),
      [`POST ${base}/quote`]: () => quote({ sufficient: false, balance: 12 }),
    });
    view();
    fireEvent.click(
      await screen.findByRole("button", { name: "Review quote" }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(screen.getByRole("alert")).toHaveTextContent("Needs 190 credits");
    expect(
      screen.getByRole("link", { name: "See credit options" }),
    ).toHaveAttribute("href", "/credits");
    expect(
      screen.getByRole("button", { name: "Generate lesson" }),
    ).toBeDisabled();
    expect(dialog).toBeInTheDocument();
  });

  it("recovers from an offline quote with Retry", async () => {
    let offline = true;
    api({
      [`GET ${base}`]: () => status(),
      [`POST ${base}/quote`]: () =>
        offline ? new TypeError("Failed to fetch") : quote(),
    });
    view();
    fireEvent.click(
      await screen.findByRole("button", { name: "Review quote" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Couldn't reach Duolify",
    );
    offline = false;
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(
      await screen.findByRole("dialog", { name: "Generate next lesson" }),
    ).toBeInTheDocument();
  });

  it("releases the sheet with Escape and returns focus to the trigger", async () => {
    api({
      [`GET ${base}`]: () => status(),
      [`POST ${base}/quote`]: () => quote(),
    });
    view();
    const open = await screen.findByRole("button", { name: "Review quote" });
    open.focus();
    fireEvent.click(open);
    const dialog = await screen.findByRole("dialog");
    fireEvent.keyDown(dialog, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(open).toHaveFocus();
  });

  it("tells the learner a failed generation was released and offers Try again", async () => {
    api({
      [`GET ${base}`]: () => status(),
      [`POST ${base}/quote`]: () => quote(),
      [`POST ${base}/generate`]: () => ({
        generation_id: "g2",
        state: "queued",
      }),
      "GET /v1/lesson-generations/g2": () => ({ state: "failed" }),
    });
    view();
    fireEvent.click(
      await screen.findByRole("button", { name: "Review quote" }),
    );
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /I agree to spend up to/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Generate lesson" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Your reserved credits were released",
    );
    expect(
      screen.getByRole("button", { name: "Try again" }),
    ).toBeInTheDocument();
  });

  it("asks for a fresh quote when the price changed", async () => {
    api({
      [`GET ${base}`]: () => status(),
      [`POST ${base}/quote`]: () => quote(),
      [`POST ${base}/generate`]: () => rejected(409, "quote_stale"),
    });
    view();
    fireEvent.click(
      await screen.findByRole("button", { name: "Review quote" }),
    );
    fireEvent.click(
      await screen.findByRole("checkbox", { name: /I agree to spend up to/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Generate lesson" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "price changed or expired",
    );
    expect(
      screen.getByRole("button", { name: "Review quote" }),
    ).toBeInTheDocument();
  });
});
