import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { PublicationDraftPanel } from "./PublicationDraftPanel";
import { draftFailure, draftMessages, type PublicationDraft } from "./drafts";
import { session } from "../accounts/session";

vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));
const request = vi.mocked(session.request);
const rejected = (status: number) =>
  Object.assign(new Error("server text"), { status });
const draft = (extra: Partial<PublicationDraft> = {}): PublicationDraft => ({
  id: "d1",
  course_id: "c1",
  number: 1,
  content_hash: "abc123",
  verdict: "pass",
  reason_codes: [],
  findings: [],
  outline: [{ title: "Unit 1", lessons: [{ title: "Orbits" }] }],
  lesson_count: 1,
  sampled_lessons: 1,
  narration_included: false,
  created_at: "2026-11-13T00:00:00Z",
  ...extra,
});

beforeEach(() => request.mockReset());

describe("publication draft preview", () => {
  it("shows a loading state, then the verdict, outline and narration note", async () => {
    request.mockResolvedValue(draft());
    render(<PublicationDraftPanel courseId="c1" onClose={() => {}} />);
    expect(screen.getByRole("status")).toHaveTextContent(/building/i);
    expect(await screen.findByText("Passed screening")).toBeInTheDocument();
    expect(screen.getByText("Orbits")).toBeInTheDocument();
    expect(
      screen.getByText(/narration audio is not included/i),
    ).toBeInTheDocument();
    expect(request).toHaveBeenCalledWith(
      "/v1/courses/c1/publication-drafts",
      "POST",
      {},
    );
  });

  it("offers no public link and a disabled publish action", async () => {
    request.mockResolvedValue(draft());
    render(<PublicationDraftPanel courseId="c1" onClose={() => {}} />);
    const publish = await screen.findByRole("button", {
      name: /public sharing is not available yet/i,
    });
    expect(publish).toBeDisabled();
    expect(screen.queryByText(/copy link/i)).toBeNull();
  });

  it("lists findings by location without any matched text", async () => {
    request.mockResolvedValue(
      draft({
        verdict: "needs_review",
        findings: [{ kind: "personal_data", location: "lesson 2 · slide 3" }],
      }),
    );
    render(<PublicationDraftPanel courseId="c1" onClose={() => {}} />);
    expect(await screen.findByText("Needs review")).toBeInTheDocument();
    expect(screen.getByText("lesson 2 · slide 3")).toBeInTheDocument();
    expect(screen.getByText("Personal data")).toBeInTheDocument();
  });

  it("shows a blocked verdict", async () => {
    request.mockResolvedValue(
      draft({ verdict: "block", reason_codes: ["prompt_injection"] }),
    );
    render(<PublicationDraftPanel courseId="c1" onClose={() => {}} />);
    expect(await screen.findByText("Blocked")).toBeInTheDocument();
  });

  it("recovers from pending screening with Retry and says learning is unaffected", async () => {
    request.mockResolvedValueOnce(draft({ verdict: "pending_screening" }));
    render(<PublicationDraftPanel courseId="c1" onClose={() => {}} />);
    expect(
      await screen.findByText(/learning is not affected/i),
    ).toBeInTheDocument();
    request.mockResolvedValueOnce(draft());
    fireEvent.click(screen.getByRole("button", { name: /retry screening/i }));
    expect(await screen.findByText("Passed screening")).toBeInTheDocument();
    expect(request).toHaveBeenLastCalledWith(
      "/v1/courses/c1/publication-drafts/d1/screen",
      "POST",
      {},
    );
  });

  it("shows the neutral unavailable state on 404", async () => {
    request.mockImplementationOnce(() => Promise.reject(rejected(404)));
    render(<PublicationDraftPanel courseId="c1" onClose={() => {}} />);
    expect(
      await screen.findByText(draftMessages.unavailable),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /retry/i })).toBeNull();
    expect(request).toHaveBeenCalledTimes(1);
  });

  it("shows an offline error with a retry that rebuilds", async () => {
    request.mockImplementationOnce(() =>
      Promise.reject(new TypeError("Failed to fetch")),
    );
    render(<PublicationDraftPanel courseId="c1" onClose={() => {}} />);
    expect(await screen.findByText(draftMessages.offline)).toBeInTheDocument();
    request.mockResolvedValueOnce(draft());
    fireEvent.click(screen.getByRole("button", { name: /try again/i }));
    expect(await screen.findByText("Passed screening")).toBeInTheDocument();
  });

  it("shows an empty state when no lessons are generated", async () => {
    request.mockResolvedValue(draft({ outline: [], lesson_count: 0 }));
    render(<PublicationDraftPanel courseId="c1" onClose={() => {}} />);
    expect(
      await screen.findByText(/no generated lessons yet/i),
    ).toBeInTheDocument();
  });

  it("moves focus into the dialog and closes with Escape", async () => {
    request.mockResolvedValue(draft());
    const onClose = vi.fn();
    render(<PublicationDraftPanel courseId="c1" onClose={onClose} />);
    await screen.findByText("Passed screening");
    await waitFor(() =>
      expect(screen.getByRole("dialog", { hidden: true })).toContainElement(
        document.activeElement as HTMLElement,
      ),
    );
    fireEvent.keyDown(screen.getByRole("dialog", { hidden: true }), {
      key: "Escape",
    });
    fireEvent(
      screen.getByRole("dialog", { hidden: true }),
      new Event("cancel", { cancelable: true }),
    );
    expect(onClose).toHaveBeenCalled();
  });

  it("classifies failures by status only", () => {
    expect(draftFailure(rejected(404))).toBe("unavailable");
    expect(draftFailure(rejected(500))).toBe("offline");
    expect(draftFailure(new TypeError("x"))).toBe("offline");
  });
});
