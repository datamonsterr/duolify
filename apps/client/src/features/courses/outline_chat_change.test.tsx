import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { OutlineChat, type ProposalService } from "./OutlineChat";
import type { OutlineDraft } from "./outline";
import { configureUiAnalytics } from "../../lib/analytics";

const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  init: vi.fn(),
}));
vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));
afterEach(() => {
  configureUiAnalytics(null);
  vi.clearAllMocks();
});

const draft: OutlineDraft = {
  id: "draft",
  brief_revision: "brief",
  revision: 2,
  outline: [{ id: "u1", title: "Manual title", lessons: [] }],
  history: [],
  confirmed: null,
};
it("requires configured quote consent, labels all diff kinds and never edits on delivery/discard", async () => {
  configureUiAnalytics({
    learnerId: "a1111111-1111-4111-8111-111111111111",
    platform: "web",
    enabled: true,
    appVersion: "0.1.0",
  });
  const proposal = {
    id: "proposal",
    draft_id: "draft",
    base_revision: 2,
    state: "delivered",
    decision: "",
    operations: ["added", "removed", "moved", "retitled"].map((kind, i) => ({
      kind,
      id: `node${i}`,
      title: "Changed",
    })),
  };
  const service: ProposalService = {
    quote: vi.fn().mockResolvedValue({
      id: "q",
      content_ceiling: 9,
      pricing_version: "unapproved",
      daily_free_eligible: true,
      expires_at: new Date(Date.now() + 600000).toISOString(),
    }),
    accept: vi.fn().mockResolvedValue({}),
    generate: vi.fn().mockResolvedValue(proposal),
    get: vi.fn(),
    decide: vi.fn().mockResolvedValue({
      proposal: { ...proposal, decision: "discarded" },
      draft,
    }),
  };
  const changed = vi.fn();
  render(
    <OutlineChat
      draft={draft}
      dirty={false}
      onDraft={changed}
      service={service}
    />,
  );
  fireEvent.change(screen.getByLabelText("Ask for an outline change"), {
    target: { value: "Change this" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Review change cost" }));
  await screen.findByText(/up to 9 credits/i);
  expect(service.generate).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole("button", { name: /Confirm.*suggest change/ }),
  );
  for (const kind of ["Added", "Removed", "Moved", "Retitled"])
    await screen.findByText(kind, { exact: true });
  expect(changed).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Discard" }));
  await waitFor(() =>
    expect(service.decide).toHaveBeenCalledWith("proposal", "discard"),
  );
  expect(changed).not.toHaveBeenCalled();
  expect(service.generate).toHaveBeenCalledTimes(1);
  expect(service.accept).toHaveBeenCalledWith("q");
  const events = posthog.capture.mock.calls;
  expect(events.map((call) => call[1].control_id)).toEqual([
    "outline_change_requested",
    "proposal_discard",
  ]);
  expect(JSON.stringify(events)).not.toMatch(
    /Change this|Changed|Manual title/,
  );
});

it("keeps failed requests and resumes the same quote without a second generation", async () => {
  const service: ProposalService = {
    quote: vi.fn().mockResolvedValue({
      id: "q",
      content_ceiling: 8,
      pricing_version: "fixture",
      daily_free_eligible: true,
      expires_at: new Date(Date.now() + 60000).toISOString(),
    }),
    accept: vi.fn(),
    generate: vi.fn().mockRejectedValue(new Error("Connection lost")),
    get: vi.fn().mockResolvedValue({
      id: "p",
      draft_id: draft.id,
      base_revision: draft.revision,
      state: "delivered",
      decision: "",
      operations: [{ kind: "retitled", id: "u1", title: "New" }],
    }),
    decide: vi.fn(),
  };
  render(
    <OutlineChat
      draft={draft}
      dirty={false}
      onDraft={vi.fn()}
      service={service}
    />,
  );
  fireEvent.change(screen.getByLabelText("Ask for an outline change"), {
    target: { value: "Private request" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Review change cost" }));
  await screen.findByText(/up to 8 credits/i);
  fireEvent.click(
    screen.getByRole("button", { name: /Confirm.*suggest change/ }),
  );
  await screen.findByRole("alert");
  expect(screen.getByLabelText("Ask for an outline change")).toHaveValue(
    "Private request",
  );
  fireEvent.click(screen.getByRole("button", { name: "Resume proposal" }));
  await screen.findByText("Retitled", { exact: true });
  expect(service.get).toHaveBeenCalledWith("", "q");
  expect(service.generate).toHaveBeenCalledTimes(1);
});

it("invalidates quote consent after draft changes and keeps manual editing usable", async () => {
  const service: ProposalService = {
    quote: vi.fn().mockResolvedValue({
      id: "q",
      content_ceiling: 8,
      pricing_version: "fixture",
      daily_free_eligible: true,
      expires_at: new Date(Date.now() + 60000).toISOString(),
    }),
    accept: vi.fn(),
    generate: vi.fn(),
    get: vi.fn(),
    decide: vi.fn(),
  };
  const { rerender } = render(
    <OutlineChat draft={draft} dirty onDraft={vi.fn()} service={service} />,
  );
  fireEvent.change(screen.getByLabelText("Ask for an outline change"), {
    target: { value: "Change" },
  });
  expect(
    screen.getByRole("button", { name: "Review change cost" }),
  ).toBeDisabled();
  rerender(
    <OutlineChat
      draft={draft}
      dirty={false}
      onDraft={vi.fn()}
      service={service}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Review change cost" }));
  await screen.findByRole("button", { name: /Confirm.*suggest change/ });
  rerender(
    <OutlineChat
      draft={{ ...draft, revision: 3 }}
      dirty={false}
      onDraft={vi.fn()}
      service={service}
    />,
  );
  expect(
    screen.queryByRole("button", { name: /Confirm.*suggest change/ }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("alert")).toHaveTextContent("Review a fresh quote");
  expect(service.generate).not.toHaveBeenCalled();
});

it("keeps the immutable before text when the manual draft changes after delivery", async () => {
  const proposal = {
    id: "p",
    draft_id: draft.id,
    base_revision: 2,
    base_outline: draft.outline,
    request: "Original private request",
    state: "delivered",
    decision: "",
    operations: [{ kind: "retitled", id: "u1", title: "Suggested" }],
  };
  const service: ProposalService = {
    quote: vi.fn().mockResolvedValue({
      id: "q",
      content_ceiling: 8,
      pricing_version: "fixture",
      daily_free_eligible: true,
      expires_at: new Date(Date.now() + 60000).toISOString(),
    }),
    accept: vi.fn(),
    generate: vi.fn().mockResolvedValue(proposal),
    get: vi.fn(),
    decide: vi.fn(),
  };
  const { rerender } = render(
    <OutlineChat
      draft={draft}
      dirty={false}
      onDraft={vi.fn()}
      service={service}
    />,
  );
  fireEvent.change(screen.getByLabelText("Ask for an outline change"), {
    target: { value: "Original private request" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Review change cost" }));
  await screen.findByRole("button", { name: /Confirm.*suggest change/ });
  fireEvent.click(
    screen.getByRole("button", { name: /Confirm.*suggest change/ }),
  );
  await screen.findByText("Manual title → Suggested");
  rerender(
    <OutlineChat
      draft={{
        ...draft,
        revision: 3,
        outline: [{ ...draft.outline[0], title: "New manual title" }],
      }}
      dirty={false}
      onDraft={vi.fn()}
      service={service}
    />,
  );
  expect(screen.getByText("Manual title → Suggested")).toBeVisible();
  expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Discard" })).toBeEnabled();
});
