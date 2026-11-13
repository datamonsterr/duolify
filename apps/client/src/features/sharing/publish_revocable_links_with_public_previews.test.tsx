import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PublicPreviewPage } from "./PublicPreviewPage";
import { ShareCoursePanel } from "./ShareCourse";
import { linkFailure, shortLink, type PublicPreview } from "./links";
import { session } from "../accounts/session";

vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));
const request = vi.mocked(session.request);
const TOKEN = "Hq7aBcDeFgHiJkLmNoPqRsTuVwX";
const created = {
  token: TOKEN,
  preview_url: `https://share.example.test/c/${TOKEN}`,
  deep_link: `duolify://share/${TOKEN}`,
};
const pass = { id: "d1", number: 2, verdict: "pass", lesson_count: 6 };
const rejected = (status: number) =>
  Object.assign(new Error("server text"), { status });

beforeEach(() => {
  request.mockReset();
  vi.restoreAllMocks();
  Object.assign(navigator, {
    clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
  });
});

describe("link helpers", () => {
  it("shortens the visible link and never shows the whole token", () => {
    const short = shortLink(created.preview_url);
    expect(short).toContain("/c/");
    expect(short).toContain("…");
    expect(short).not.toContain(TOKEN);
  });
  it("classifies failures by status only", () => {
    expect(linkFailure(rejected(404))).toBe("unavailable");
    expect(linkFailure(rejected(429))).toBe("rate_limited");
    expect(linkFailure(new TypeError("Failed to fetch"))).toBe("offline");
  });
});

describe("share panel", () => {
  const open = () =>
    render(<ShareCoursePanel courseId="c1" onClose={() => {}} />);

  it("defaults to private with no link when none is active", async () => {
    request.mockResolvedValueOnce({ visibility: "private", active: false });
    open();
    expect(await screen.findByRole("radio", { name: "Private" })).toBeChecked();
    expect(screen.queryByTestId("share-link-url")).toBeNull();
    expect(
      screen.getByRole("radio", { name: /Public in marketplace/ }),
    ).toBeDisabled();
  });

  it("screens a fresh draft, publishes, and shows the shortened link once", async () => {
    request
      .mockResolvedValueOnce({ visibility: "private", active: false })
      .mockResolvedValueOnce(pass)
      .mockResolvedValueOnce(created);
    open();
    fireEvent.click(
      await screen.findByRole("radio", { name: /Anyone with the link/ }),
    );
    expect(await screen.findByTestId("share-link-url")).toHaveTextContent(
      "/c/",
    );
    expect(screen.getByTestId("share-link-url")).not.toHaveTextContent(TOKEN);
    expect(request).toHaveBeenCalledWith(
      "/v1/courses/c1/publication-drafts",
      "POST",
      {},
    );
    expect(request).toHaveBeenCalledWith(
      "/v1/courses/c1/share-link",
      "POST",
      {},
    );
    expect(screen.getByText(/Version 2 · 6 lessons ready/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Copy link" }));
    await waitFor(() =>
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
        created.preview_url,
      ),
    );
    expect(await screen.findByText("Link copied")).toBeInTheDocument();
  });

  it("keeps a draft that needs review private and offers no link", async () => {
    request
      .mockResolvedValueOnce({ visibility: "private", active: false })
      .mockResolvedValueOnce({ ...pass, verdict: "needs_review" });
    open();
    fireEvent.click(
      await screen.findByRole("radio", { name: /Anyone with the link/ }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Needs a quick review/,
    );
    expect(screen.queryByTestId("share-link-url")).toBeNull();
    expect(screen.getByRole("radio", { name: "Private" })).toBeChecked();
    expect(request).not.toHaveBeenCalledWith(
      "/v1/courses/c1/share-link",
      "POST",
      {},
    );
  });

  it("rotates and revokes through the owner endpoints", async () => {
    request.mockResolvedValueOnce({
      visibility: "link",
      active: true,
      version: 2,
      lesson_count: 6,
    });
    open();
    expect(
      await screen.findByRole("radio", { name: /Anyone with the link/ }),
    ).toBeChecked();
    expect(screen.queryByRole("button", { name: "Copy link" })).toBeNull();
    request.mockResolvedValueOnce({ ...created, token: TOKEN + "2" });
    fireEvent.click(screen.getByRole("button", { name: "New link" }));
    expect(await screen.findByText("Link replaced")).toBeInTheDocument();
    expect(request).toHaveBeenCalledWith(
      "/v1/courses/c1/share-link/rotate",
      "POST",
      {},
    );
    request.mockResolvedValueOnce(undefined);
    fireEvent.click(screen.getByRole("button", { name: "Revoke link" }));
    expect(await screen.findByText("Link revoked")).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Private" })).toBeChecked();
    expect(request).toHaveBeenLastCalledWith(
      "/v1/courses/c1/share-link",
      "DELETE",
    );
  });

  it("offers Retry when offline", async () => {
    request.mockRejectedValueOnce(new TypeError("Failed to fetch"));
    open();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Couldn't reach Duolify/,
    );
    request.mockResolvedValueOnce({ visibility: "private", active: false });
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("radio", { name: "Private" })).toBeChecked();
  });
});

describe("public preview", () => {
  const payload = (extra: Partial<PublicPreview> = {}): PublicPreview => ({
    course_id: "c1",
    title: "Distributed systems",
    description: "From consensus to queues.",
    version: 3,
    lesson_count: 24,
    generated_lesson_count: 6,
    creator: { username: "mina", display_name: "Mina" },
    outline: [
      { title: "Unit 1", lessons: [{ title: "Raft", generated: true }] },
    ],
    sample_slides: [1, 2, 3].map((n) => ({
      id: `s${n}`,
      html: `<h1>Slide ${n}</h1>`,
    })),
    enrollment_count: 1200,
    ...extra,
  });
  const respond = (status: number, body: unknown = {}) =>
    vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response(JSON.stringify(body), { status }));

  it("shows creator, version, disclosure and pages sample slides", async () => {
    respond(200, payload());
    render(<PublicPreviewPage token={TOKEN} />);
    expect(
      await screen.findByRole("heading", { name: "Distributed systems" }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("creator")).toHaveTextContent("by @mina");
    expect(screen.getByText(/version 3/i)).toBeInTheDocument();
    expect(screen.getByTestId("lessons-ready")).toHaveTextContent(
      "6 of 24 lessons ready",
    );
    expect(screen.getByText(/Ready lessons are free/)).toBeInTheDocument();
    expect(screen.getByText("Preview 1 of 3")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Next slide" }));
    expect(screen.getByText("Preview 2 of 3")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Previous slide" }),
    ).toBeEnabled();
    expect(
      screen.getByRole("link", { name: "Open in Duolify app" }),
    ).toHaveAttribute("href", `duolify://share/${TOKEN}`);
    expect(
      screen.getByRole("link", { name: "Sign in to enroll" }),
    ).toHaveAttribute("href", expect.stringContaining("/sign-in?returnTo="));
  });

  it("sandboxes slide HTML so it cannot script the page", async () => {
    respond(200, payload());
    render(<PublicPreviewPage token={TOKEN} />);
    const frame = await screen.findByTitle("Sample slide 1");
    expect(frame.getAttribute("sandbox")).toBe("");
    expect(frame.getAttribute("srcdoc")).toContain("Slide 1");
  });

  it("marks the page noindex while mounted and requests no credentials", async () => {
    const spy = respond(200, payload());
    const { unmount } = render(<PublicPreviewPage token={TOKEN} />);
    await screen.findByRole("heading", { name: "Distributed systems" });
    expect(
      document.head
        .querySelector('meta[name="robots"]')
        ?.getAttribute("content"),
    ).toMatch(/noindex/);
    const init = spy.mock.calls[0]![1] as RequestInit | undefined;
    expect(init?.credentials).toBe("omit");
    unmount();
    expect(document.head.querySelector('meta[name="robots"]')).toBeNull();
  });

  it("shows the neutral revoked state for any 404", async () => {
    respond(404, { error: "link_not_found" });
    render(<PublicPreviewPage token={TOKEN} />);
    expect(
      await screen.findByText(
        "This course link is no longer active. Ask the creator for a new one.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  });

  it("explains rate limiting and offers Retry offline", async () => {
    respond(429, { error: "rate_limited" });
    render(<PublicPreviewPage token={TOKEN} />);
    expect(await screen.findByRole("alert")).toHaveTextContent(/too many/i);
    vi.spyOn(globalThis, "fetch").mockRejectedValueOnce(new TypeError("x"));
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Couldn't reach Duolify/,
    );
  });

  it("copes with a long title, no slides and no outline", async () => {
    respond(
      200,
      payload({ title: "L".repeat(300), sample_slides: [], outline: [] }),
    );
    render(<PublicPreviewPage token={TOKEN} />);
    expect(
      await screen.findByRole("heading", { level: 1 }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Preview 1 of/)).toBeNull();
    expect(screen.getByText(/No sample slides yet/)).toBeInTheDocument();
  });
});
