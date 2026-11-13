import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MarketplacePage } from "./MarketplacePage";
import { MarketCoursePage } from "./MarketCoursePage";
import {
  catalogueUrl,
  learnersLabel,
  marketFailure,
  marketMessages,
  ratingLabel,
  type CatalogueItem,
} from "./market";
import { session } from "../accounts/session";

vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));
const request = vi.mocked(session.request);
const rejected = (status: number) =>
  Object.assign(new Error("server text: pq password"), { status });

const item = (
  n: number,
  patch: Partial<CatalogueItem> = {},
): CatalogueItem => ({
  course_id: `c${n}`,
  title: `Course ${n}`,
  description: "From consensus to queues.",
  category: "tech",
  language: "en",
  version: 3,
  published_at: "2026-11-02T10:00:00Z",
  creator: { username: "mina", display_name: "Mina" },
  lessons_ready: 6,
  lessons_total: 24,
  enrollment_count: 1200,
  rating_average: 4.8,
  rating_count: 412,
  ...patch,
});
const page = (items: CatalogueItem[], next_cursor = "") => ({
  items,
  next_cursor,
});

beforeEach(() => {
  request.mockReset();
  window.history.replaceState(null, "", "/marketplace");
});

describe("catalogue url and labels", () => {
  it("omits empty filters, always sends the sort and encodes the query", () => {
    expect(catalogueUrl({})).toBe("/v1/marketplace/courses?sort=popular");
    expect(
      catalogueUrl({
        q: "rust & go",
        category: "tech",
        language: "es",
        sort: "rating",
        cursor: "abc",
      }),
    ).toBe(
      "/v1/marketplace/courses?q=rust+%26+go&category=tech&language=es&sort=rating&cursor=abc",
    );
    expect(catalogueUrl({ q: "   ", category: "" })).toBe(
      "/v1/marketplace/courses?sort=popular",
    );
  });

  it("shows New for an unrated course and aggregate counts only", () => {
    expect(ratingLabel(item(1))).toBe("★ 4.8 (412)");
    expect(
      ratingLabel(item(1, { rating_average: null, rating_count: 0 })),
    ).toBe("New");
    expect(learnersLabel(0)).toBe("No learners yet");
    expect(learnersLabel(1)).toBe("1 learner");
    expect(learnersLabel(980)).toBe("980 learners");
    expect(learnersLabel(1200)).toBe("1.2k learners");
  });

  it("classifies failures by status and never shows server text", () => {
    expect(marketFailure(rejected(404))).toBe("unavailable");
    expect(marketFailure(rejected(429))).toBe("rate_limited");
    expect(marketFailure(rejected(500))).toBe("offline");
    expect(marketFailure(new TypeError("Failed to fetch"))).toBe("offline");
    for (const text of Object.values(marketMessages))
      expect(text).not.toContain("server text");
  });
});

describe("marketplace browse", () => {
  it("lists cards with attribution, rating, learners and ready lessons", async () => {
    request.mockResolvedValueOnce(
      page([item(1), item(2, { rating_count: 0, rating_average: null })]),
    );
    render(<MarketplacePage />);
    expect(screen.getByRole("status")).toHaveTextContent("Loading courses");
    const results = await screen.findByTestId("market-results");
    expect(results).toHaveTextContent("Course 1");
    expect(screen.getByRole("link", { name: "Course 1" })).toHaveAttribute(
      "href",
      "/marketplace/c1",
    );
    expect(results).toHaveTextContent("@mina");
    expect(results).toHaveTextContent("★ 4.8 (412)");
    expect(results).toHaveTextContent("1.2k learners");
    expect(results).toHaveTextContent("6 of 24 lessons ready");
    expect(results).toHaveTextContent("New");
    expect(request).toHaveBeenCalledWith(
      "/v1/marketplace/courses?sort=popular",
    );
  });

  it("searches on submit, keeps the query and filters in the URL", async () => {
    request.mockResolvedValue(page([item(1)]));
    render(<MarketplacePage />);
    await screen.findByTestId("market-results");
    const box = screen.getByRole("searchbox", {
      name: "Search public courses",
    });
    fireEvent.change(box, { target: { value: "rust" } });
    expect(request).toHaveBeenCalledTimes(1); // typing alone does not search
    fireEvent.submit(box.closest("form")!);
    await waitFor(() =>
      expect(request).toHaveBeenLastCalledWith(
        "/v1/marketplace/courses?q=rust&sort=popular",
      ),
    );
    fireEvent.click(screen.getByRole("button", { name: "Tech" }));
    await waitFor(() =>
      expect(request).toHaveBeenLastCalledWith(
        "/v1/marketplace/courses?q=rust&category=tech&sort=popular",
      ),
    );
    expect(screen.getByRole("button", { name: "Tech" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    fireEvent.change(screen.getByLabelText("Language"), {
      target: { value: "es" },
    });
    fireEvent.change(screen.getByLabelText("Sort"), {
      target: { value: "rating" },
    });
    await waitFor(() =>
      expect(request).toHaveBeenLastCalledWith(
        "/v1/marketplace/courses?q=rust&category=tech&language=es&sort=rating",
      ),
    );
    expect(window.location.search).toBe(
      "?q=rust&category=tech&language=es&sort=rating",
    );
    fireEvent.click(screen.getByRole("button", { name: "All" }));
    await waitFor(() =>
      expect(request).toHaveBeenLastCalledWith(
        "/v1/marketplace/courses?q=rust&language=es&sort=rating",
      ),
    );
  });

  it("starts from the URL so Back returns to the same results", async () => {
    window.history.replaceState(
      null,
      "",
      "/marketplace?q=pasta&sort=new&category=life_skills",
    );
    request.mockResolvedValue(page([item(1)]));
    render(<MarketplacePage />);
    await screen.findByTestId("market-results");
    expect(request).toHaveBeenCalledWith(
      "/v1/marketplace/courses?q=pasta&category=life_skills&sort=new",
    );
    expect(
      screen.getByRole("searchbox", { name: "Search public courses" }),
    ).toHaveValue("pasta");
    expect(screen.getByLabelText("Sort")).toHaveValue("new");
  });

  it("loads more with the cursor, appends once and hides the button at the end", async () => {
    request
      .mockResolvedValueOnce(page([item(1), item(2)], "cur-1"))
      .mockResolvedValueOnce(page([item(2), item(3)], ""));
    render(<MarketplacePage />);
    await screen.findByTestId("market-results");
    fireEvent.click(screen.getByRole("button", { name: "Load more" }));
    await waitFor(() =>
      expect(request).toHaveBeenLastCalledWith(
        "/v1/marketplace/courses?sort=popular&cursor=cur-1",
      ),
    );
    await waitFor(() => expect(screen.getAllByRole("article")).toHaveLength(3));
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
  });

  it("keeps the query on an empty result and offers to create the course", async () => {
    request.mockResolvedValue(page([]));
    render(<MarketplacePage />);
    const box = screen.getByRole("searchbox", {
      name: "Search public courses",
    });
    fireEvent.change(box, { target: { value: "quantum knitting" } });
    fireEvent.submit(box.closest("form")!);
    const empty = await screen.findByTestId("market-empty");
    expect(empty).toHaveTextContent("quantum knitting");
    expect(
      screen.getByRole("link", { name: "Create this course" }),
    ).toHaveAttribute("href", "/courses/create?topic=quantum%20knitting");
    expect(box).toHaveValue("quantum knitting");
  });

  it("shows a neutral alert with Retry, then recovers", async () => {
    request.mockRejectedValueOnce(rejected(500));
    render(<MarketplacePage />);
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(marketMessages.offline);
    expect(alert).not.toHaveTextContent("server text");
    request.mockResolvedValueOnce(page([item(1)]));
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByTestId("market-results")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("ignores a slow response that was overtaken by a newer search", async () => {
    let first!: (v: unknown) => void;
    request.mockImplementationOnce(() => new Promise((r) => (first = r)));
    render(<MarketplacePage />);
    request.mockResolvedValueOnce(page([item(9, { title: "Newest search" })]));
    const box = screen.getByRole("searchbox", {
      name: "Search public courses",
    });
    fireEvent.change(box, { target: { value: "new" } });
    fireEvent.submit(box.closest("form")!);
    await screen.findByText("Newest search");
    await act(async () => first(page([item(1, { title: "Stale result" })])));
    expect(screen.queryByText("Stale result")).toBeNull();
    expect(screen.getByText("Newest search")).toBeInTheDocument();
  });
});

const detail = (patch: Record<string, unknown> = {}) => ({
  ...item(1, { title: "Distributed systems" }),
  tags: ["raft"],
  outline: [
    {
      title: "Unit 1 · What can go wrong",
      lessons: [
        { title: "Failures", generated: true },
        { title: "Clocks", generated: false },
      ],
    },
  ],
  sample_slides: [
    { id: "s1", html: "<h1>Slide one</h1>" },
    { id: "s2", html: "<h1>Slide two</h1>" },
  ],
  viewer: { enrolled: false, owner: false },
  ...patch,
});

describe("marketplace course page", () => {
  it("shows attribution, version, outline, safe previews and who pays for the rest", async () => {
    request.mockResolvedValueOnce(detail());
    render(<MarketCoursePage id="c1" />);
    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "Distributed systems",
      }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("creator")).toHaveTextContent("by @mina · v3");
    expect(screen.getByTestId("lessons-ready")).toHaveTextContent(
      "6 of 24 lessons ready",
    );
    expect(screen.getByTestId("paid-note")).toHaveTextContent(
      /generated as you learn and cost Credits/i,
    );
    expect(screen.getByTestId("paid-note")).not.toHaveTextContent(
      /\d+ credits/i,
    );
    const frame = screen.getByTitle("Sample slide 1") as HTMLIFrameElement;
    expect(frame.getAttribute("sandbox")).toBe("");
    expect(
      screen.getByRole("list", { name: "Course outline" }),
    ).toHaveTextContent("Clocks · generated as you learn");
    fireEvent.click(screen.getByRole("button", { name: "Next slide" }));
    expect(screen.getByTitle("Sample slide 2")).toBeInTheDocument();
    expect(request).toHaveBeenCalledWith("/v1/marketplace/courses/c1");
  });

  it("enrolls for free once and links to the pinned copy", async () => {
    request.mockResolvedValueOnce(detail());
    render(<MarketCoursePage id="c1" />);
    const button = await screen.findByRole("button", { name: /^Enroll/ });
    expect(button).toHaveTextContent("free to start");
    request.mockResolvedValueOnce({
      course_id: "copy-1",
      created: true,
      version: 3,
      total_lessons: 24,
      ready_lessons: 6,
      needs_generation_lessons: 18,
    });
    fireEvent.click(button);
    fireEvent.click(button);
    expect(await screen.findByTestId("enroll-status")).toHaveTextContent(
      "Enrolled",
    );
    expect(screen.getByRole("link", { name: "Open course" })).toHaveAttribute(
      "href",
      "/course/copy-1",
    );
    expect(request).toHaveBeenCalledWith(
      "/v1/marketplace/courses/c1/enroll",
      "POST",
      {},
    );
    expect(request.mock.calls.filter((c) => c[1] === "POST")).toHaveLength(1);
  });

  it("recognises an existing copy and the owner's own course", async () => {
    request.mockResolvedValueOnce(
      detail({ viewer: { enrolled: true, owner: false, course_id: "copy-9" } }),
    );
    const { unmount } = render(<MarketCoursePage id="c1" />);
    expect(await screen.findByTestId("enroll-status")).toHaveTextContent(
      "Already enrolled",
    );
    expect(screen.getByRole("link", { name: "Open course" })).toHaveAttribute(
      "href",
      "/course/copy-9",
    );
    unmount();
    request.mockResolvedValueOnce(
      detail({ viewer: { enrolled: false, owner: true } }),
    );
    render(<MarketCoursePage id="c1" />);
    expect(await screen.findByText(/This is your course/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Enroll/ })).toBeNull();
  });

  it("shows one neutral page for a delisted, unknown or hidden course", async () => {
    request.mockRejectedValueOnce(rejected(404));
    render(<MarketCoursePage id="c1" />);
    expect(await screen.findByRole("heading", { level: 1 })).toHaveTextContent(
      "This course isn't available",
    );
    expect(
      screen.getByRole("link", { name: "Back to Marketplace" }),
    ).toHaveAttribute("href", "/marketplace");
  });

  it("offers Retry on a network failure and a clear message when enrolling is rate limited", async () => {
    request.mockRejectedValueOnce(rejected(500));
    render(<MarketCoursePage id="c1" />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      marketMessages.offline,
    );
    request.mockResolvedValueOnce(detail());
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    const enroll = await screen.findByRole("button", { name: /^Enroll/ });
    request.mockRejectedValueOnce(rejected(429));
    fireEvent.click(enroll);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      marketMessages.rate_limited,
    );
    expect(screen.getByRole("button", { name: /^Enroll/ })).toBeEnabled();
  });
});
