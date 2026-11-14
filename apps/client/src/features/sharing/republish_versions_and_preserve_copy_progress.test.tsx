import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { session } from "../accounts/session";
import { ArchivedLessons } from "./ArchivedLessons";
import { CourseUpdate } from "./CourseUpdate";
import { RepublishPanel } from "./RepublishPanel";
import { republishFailure, updateFailure } from "./republish";

vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));
const request = vi.mocked(session.request);
const rejected = (status: number) =>
  Object.assign(new Error("server text"), { status });

const versions = {
  versions: [
    {
      number: 1,
      verdict: "pass",
      live: true,
      lesson_count: 3,
      changelog: "",
      created_at: "2026-11-01T00:00:00Z",
    },
  ],
  republishes_remaining: 2,
  limit: 3,
};
const info = {
  update_available: true,
  forked: false,
  current_version: 1,
  latest_version: 2,
  lock_version: 4,
  changelog: [
    {
      version: 2,
      text: "Rewrote lesson 2.",
      added: 1,
      removed: 1,
      retitled: 1,
    },
  ],
  completed_lessons: 5,
  archived_count: 0,
};

/** Routes by method and path so the order of calls never matters. */
function api(routes: Record<string, unknown | (() => unknown)>) {
  request.mockImplementation(async (path: string, method?: string) => {
    const key = `${method ?? "GET"} ${path}`;
    if (!(key in routes)) throw rejected(500);
    const value = routes[key];
    const out =
      typeof value === "function" ? (value as () => unknown)() : value;
    if (out instanceof Error) throw out;
    return out as never;
  });
}

beforeEach(() => request.mockReset());

describe("failure classification", () => {
  it("maps statuses only, never server text", () => {
    expect(republishFailure(rejected(429))).toBe("limit");
    expect(republishFailure(rejected(409))).toBe("not_published");
    expect(republishFailure(rejected(403))).toBe("adult");
    expect(republishFailure(rejected(404))).toBe("unavailable");
    expect(republishFailure(new TypeError("Failed to fetch"))).toBe("offline");
    expect(updateFailure(rejected(409))).toBe("stale");
    expect(updateFailure(rejected(404))).toBe("unavailable");
    expect(updateFailure(new TypeError("Failed to fetch"))).toBe("offline");
  });
});

describe("republish panel", () => {
  it("shows the remaining daily budget and publishes with the changelog", async () => {
    api({
      "GET /v1/courses/c1/versions": versions,
      "POST /v1/courses/c1/republish": {
        id: "d2",
        number: 2,
        verdict: "pass",
        lesson_count: 4,
      },
    });
    render(<RepublishPanel courseId="c1" />);
    expect(
      await screen.findByText(/2 of 3 new versions left today/),
    ).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("What's new in this version"), {
      target: { value: "Added comets." },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Publish new version" }),
    );
    expect(await screen.findByText("Version 2 is live")).toBeInTheDocument();
    expect(screen.getByText(/Learners on version 1 see/)).toBeInTheDocument();
    expect(request).toHaveBeenCalledWith("/v1/courses/c1/republish", "POST", {
      changelog: "Added comets.",
    });
  });

  it("keeps the old version live when screening blocks the new one", async () => {
    api({
      "GET /v1/courses/c1/versions": versions,
      "POST /v1/courses/c1/republish": {
        id: "d2",
        number: 2,
        verdict: "block",
        lesson_count: 4,
      },
    });
    render(<RepublishPanel courseId="c1" />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Publish new version" }),
    );
    expect(
      await screen.findByText("This version can't be published"),
    ).toBeInTheDocument();
    expect(screen.getByText(/Version 1 stays live/)).toBeInTheDocument();
    expect(screen.queryByText("Version 2 is live")).toBeNull();
  });

  it("explains a pending screening without publishing", async () => {
    api({
      "GET /v1/courses/c1/versions": versions,
      "POST /v1/courses/c1/republish": {
        id: "d2",
        number: 2,
        verdict: "pending_screening",
        lesson_count: 4,
      },
    });
    render(<RepublishPanel courseId="c1" />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Publish new version" }),
    );
    expect(
      await screen.findByText("Checking version 2 for safety"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Version 1 stays live meanwhile/),
    ).toBeInTheDocument();
  });

  it("disables the button with the reason when the daily limit is spent", async () => {
    api({
      "GET /v1/courses/c1/versions": { ...versions, republishes_remaining: 0 },
    });
    render(<RepublishPanel courseId="c1" />);
    expect(
      await screen.findByText(/0 of 3 new versions left today/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Publish new version" }),
    ).toBeDisabled();
  });

  it("shows the limit error from the server and offline errors with retry", async () => {
    let fail: Error = rejected(429);
    api({
      "GET /v1/courses/c1/versions": versions,
      "POST /v1/courses/c1/republish": () => fail,
    });
    render(<RepublishPanel courseId="c1" />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Publish new version" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(/limit/i);
    fail = new TypeError("Failed to fetch");
    fireEvent.click(
      screen.getByRole("button", { name: "Publish new version" }),
    );
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        /Couldn't reach Duolify/,
      ),
    );
  });

  it("recovers from a failed load", async () => {
    api({ "GET /v1/courses/c1/versions": new TypeError("Failed to fetch") });
    render(<RepublishPanel courseId="c1" />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Couldn't reach Duolify/,
    );
    api({ "GET /v1/courses/c1/versions": versions });
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(
      await screen.findByRole("button", { name: "Publish new version" }),
    ).toBeInTheDocument();
  });
});

describe("course update sheet", () => {
  const open = async () => {
    fireEvent.click(
      await screen.findByRole("button", { name: "Update available" }),
    );
    return screen.findByTestId("course-update");
  };

  it("offers nothing when the copy is current", async () => {
    api({ "GET /v1/courses/k1/update": { ...info, update_available: false } });
    const { container } = render(
      <CourseUpdate courseId="k1" onUpdated={() => {}} />,
    );
    await waitFor(() => expect(request).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it("offers nothing to a forked copy and says why", async () => {
    api({
      "GET /v1/courses/k1/update": {
        ...info,
        forked: true,
        update_available: false,
      },
    });
    render(<CourseUpdate courseId="k1" onUpdated={() => {}} />);
    expect(
      await screen.findByText(
        "You've personalized this course, so it no longer receives updates.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Update available" }),
    ).toBeNull();
  });

  it("shows the changelog and the progress promise, then updates with the lock version", async () => {
    const onUpdated = vi.fn();
    api({
      "GET /v1/courses/k1/update": info,
      "POST /v1/courses/k1/update": {
        version: 2,
        lock_version: 5,
        kept: 5,
        added: 1,
        archived: 1,
      },
    });
    render(<CourseUpdate courseId="k1" onUpdated={onUpdated} />);
    const sheet = await open();
    expect(sheet).toHaveTextContent("Version 2 is available");
    expect(sheet).toHaveTextContent("You're on version 1.");
    expect(sheet).toHaveTextContent("Rewrote lesson 2.");
    expect(sheet).toHaveTextContent(
      "Your 5 completed lessons stay completed. Lessons removed upstream stay in your history as archived.",
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Update to version 2" }),
    );
    await waitFor(() => expect(onUpdated).toHaveBeenCalled());
    expect(request).toHaveBeenCalledWith("/v1/courses/k1/update", "POST", {
      lock_version: 4,
      to_version: 2,
    });
  });

  it("closes on Stay on version 1 and on Escape without calling the API", async () => {
    api({ "GET /v1/courses/k1/update": info });
    render(<CourseUpdate courseId="k1" onUpdated={() => {}} />);
    await open();
    fireEvent.click(screen.getByRole("button", { name: "Stay on version 1" }));
    expect(screen.queryByTestId("course-update")).toBeNull();
    await open();
    fireEvent.keyDown(screen.getByTestId("course-update"), { key: "Escape" });
    expect(screen.queryByTestId("course-update")).toBeNull();
    expect(request).not.toHaveBeenCalledWith(
      "/v1/courses/k1/update",
      "POST",
      expect.anything(),
    );
  });

  it("rejects a stale update neutrally and reloads the offer", async () => {
    let reload = info;
    api({
      "GET /v1/courses/k1/update": () => reload,
      "POST /v1/courses/k1/update": () => {
        reload = { ...info, lock_version: 9, latest_version: 3 };
        return rejected(409);
      },
    });
    render(<CourseUpdate courseId="k1" onUpdated={() => {}} />);
    await open();
    fireEvent.click(
      screen.getByRole("button", { name: "Update to version 2" }),
    );
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(
      "This course changed while you were updating. Review the latest version and try again.",
    );
    expect(alert).not.toHaveTextContent("server text");
    expect(
      await screen.findByText("Version 3 is available"),
    ).toBeInTheDocument();
  });

  it("recovers from an offline error with Retry", async () => {
    const onUpdated = vi.fn();
    let fail = true;
    api({
      "GET /v1/courses/k1/update": info,
      "POST /v1/courses/k1/update": () =>
        fail
          ? new TypeError("Failed to fetch")
          : { version: 2, lock_version: 5, kept: 5, added: 0, archived: 0 },
    });
    render(<CourseUpdate courseId="k1" onUpdated={onUpdated} />);
    await open();
    fireEvent.click(
      screen.getByRole("button", { name: "Update to version 2" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Couldn't reach Duolify/,
    );
    fail = false;
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(onUpdated).toHaveBeenCalled());
  });

  it("uses the singular for one completed lesson", async () => {
    api({ "GET /v1/courses/k1/update": { ...info, completed_lessons: 1 } });
    render(<CourseUpdate courseId="k1" onUpdated={() => {}} />);
    expect(await open()).toHaveTextContent(
      "Your 1 completed lesson stays completed.",
    );
  });

  it("sends no client analytics: the API owns course_update_applied", async () => {
    api({
      "GET /v1/courses/k1/update": info,
      "POST /v1/courses/k1/update": {
        version: 2,
        lock_version: 5,
        kept: 5,
        added: 0,
        archived: 0,
      },
    });
    render(<CourseUpdate courseId="k1" onUpdated={() => {}} />);
    await open();
    fireEvent.click(
      screen.getByRole("button", { name: "Update to version 2" }),
    );
    await waitFor(() => expect(request).toHaveBeenCalledTimes(2));
    for (const call of request.mock.calls)
      expect(String(call[0])).not.toMatch(/analytics|events/);
  });
});

describe("archived lessons history", () => {
  it("renders nothing without archived lessons", () => {
    const { container } = render(<ArchivedLessons courseId="k1" count={0} />);
    expect(container).toBeEmptyDOMElement();
    expect(request).not.toHaveBeenCalled();
  });

  it("lists archived lessons on demand with a recoverable error", async () => {
    api({
      "GET /v1/courses/k1/archived-lessons": new TypeError("Failed to fetch"),
    });
    render(<ArchivedLessons courseId="k1" count={1} />);
    fireEvent.click(screen.getByText("Archived lessons"));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Couldn't reach Duolify/,
    );
    api({
      "GET /v1/courses/k1/archived-lessons": {
        lessons: [
          {
            title: "Eclipses",
            archived_at: "2026-11-14T00:00:00Z",
            version: 2,
            completed: false,
          },
        ],
      },
    });
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByText("Eclipses")).toBeInTheDocument();
  });
});
