import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { OutlineEditor, type Outline } from "../design/chat/OutlineEditor";
import { configureUiAnalytics } from "../../lib/analytics";
import { CourseOutline } from "./CourseOutline";
import { AuthError } from "../accounts/session";
import type { OutlineDraft, OutlineService } from "./outline";

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
const outline: Outline = [
  {
    id: "u1",
    title: "Moon",
    lessons: [{ id: "l1", title: "Phases", goal: "Recognize phases" }],
  },
  { id: "u2", title: "Stars", lessons: [] },
];

it("restores bounded history, keeps move focus and announces position without leaking text", () => {
  configureUiAnalytics({
    learnerId: "a1111111-1111-4111-8111-111111111111",
    platform: "web",
    enabled: true,
    appVersion: "0.1.0",
  });
  const change = vi.fn();
  render(
    <OutlineEditor
      initialValue={outline}
      initialHistory={[{ value: [outline[0]], focusId: "u1" }]}
      onChange={change}
      persistent
    />,
  );
  const move = screen.getByRole("button", { name: "Move Stars up" });
  fireEvent.click(screen.getByRole("button", { name: "Collapse Moon" }));
  expect(screen.queryByLabelText("Title for Phases")).not.toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Expand Moon" }));
  expect(screen.getByLabelText("Title for Phases")).toBeVisible();
  move.focus();
  fireEvent.click(move);
  expect(move).toHaveFocus();
  expect(screen.getByRole("status")).toHaveTextContent(
    "Stars moved to position 1",
  );
  expect(
    change.mock.calls[change.mock.calls.length - 1]?.[0].map(
      (u: { id: string }) => u.id,
    ),
  ).toEqual(["u2", "u1"]);
  const history = change.mock.calls[change.mock.calls.length - 1]?.[1];
  expect(history).toHaveLength(2);
  fireEvent.click(screen.getByRole("button", { name: "Undo" }));
  expect(change.mock.calls[change.mock.calls.length - 1]?.[0]).toEqual(outline);
  expect(posthog.capture).toHaveBeenCalledWith(
    "ui_control_activated",
    expect.objectContaining({ surface: "outline", control_id: "outline_move" }),
  );
  expect(JSON.stringify(posthog.capture.mock.calls)).not.toMatch(
    /Moon|Stars|Recognize phases/,
  );
  expect(posthog.capture).toHaveBeenCalledWith(
    "ui_control_activated",
    expect.objectContaining({
      distinct_id: "a1111111-1111-4111-8111-111111111111",
      source: "client",
      platform: "web",
      app_version: "0.1.0",
    }),
  );
});

it("creates unique IDs across remounts and edits nonempty goals, with opt-out", () => {
  configureUiAnalytics({
    learnerId: "a1111111-1111-4111-8111-111111111111",
    platform: "web",
    enabled: false,
    appVersion: "0.1.0",
  });
  const change = vi.fn();
  const view = render(
    <OutlineEditor initialValue={outline} onChange={change} persistent />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Add a lesson to Moon" }));
  const first = change.mock.calls[change.mock.calls.length - 1]?.[0];
  expect(first[0].lessons.at(-1).goal.trim()).not.toBe("");
  fireEvent.change(screen.getByLabelText("Goal for New lesson"), {
    target: { value: "Observe the Moon" },
  });
  view.unmount();
  render(<OutlineEditor initialValue={first} onChange={change} persistent />);
  fireEvent.click(screen.getByRole("button", { name: "Add a lesson to Moon" }));
  const ids = change.mock.calls[
    change.mock.calls.length - 1
  ]?.[0][0].lessons.map((l: { id: string }) => l.id);
  expect(new Set(ids).size).toBe(ids.length);
  expect(posthog.capture).not.toHaveBeenCalled();
});

it("bounds history to 50 valid edits and makes an applied proposal undoable", () => {
  const change = vi.fn();
  const value = [outline[0]];
  const view = render(
    <OutlineEditor
      persistent
      initialValue={value}
      initialHistory={Array.from({ length: 50 }, () => ({
        value,
        focusId: "u1",
      }))}
      onChange={change}
    />,
  );
  fireEvent.change(screen.getByLabelText("Title for Moon"), {
    target: { value: "" },
  });
  fireEvent.change(screen.getByLabelText("Title for Untitled unit"), {
    target: { value: "A new Moon" },
  });
  const stack = change.mock.calls[change.mock.calls.length - 1][1];
  expect(stack).toHaveLength(50);
  expect(
    stack.every((entry: { value: Outline }) =>
      entry.value.every((unit) => unit.title.trim()),
    ),
  ).toBe(true);
  const proposed = [{ ...value[0], title: "Applied proposal" }];
  view.rerender(
    <OutlineEditor
      persistent
      initialValue={value}
      onChange={change}
      proposal={proposed}
    />,
  );
  expect(screen.getByLabelText("Title for Applied proposal")).toHaveValue(
    "Applied proposal",
  );
  fireEvent.click(screen.getByRole("button", { name: "Undo" }));
  expect(screen.getByLabelText("Title for A new Moon")).toHaveValue(
    "A new Moon",
  );
});

const savedDraft: OutlineDraft = {
  id: "draft",
  brief_revision: "brief",
  revision: 3,
  outline: [outline[0]],
  history: [],
  confirmed: null,
};
function fakeService(): OutlineService {
  return {
    latest: vi.fn().mockResolvedValue(null),
    create: vi.fn().mockResolvedValue(savedDraft),
    save: vi.fn().mockResolvedValue({ ...savedDraft, revision: 4 }),
    confirm: vi.fn().mockImplementation((draft: OutlineDraft) =>
      Promise.resolve({
        draft_id: draft.id,
        revision: draft.revision,
        content_hash: "hash",
        outline: draft.outline,
      }),
    ),
  };
}
it("keeps unsaved edits on failure and confirms only the returned saved revision", async () => {
  const service = fakeService();
  const handoff = vi.fn();
  vi.mocked(service.create).mockRejectedValueOnce(new Error("Offline"));
  render(
    <CourseOutline
      briefRevision="brief"
      initialValue={[outline[0]]}
      onBack={vi.fn()}
      onConfirmed={handoff}
      service={service}
    />,
  );
  fireEvent.change(screen.getByLabelText("Title for Moon"), {
    target: { value: "Edited Moon" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Confirm outline" }));
  await screen.findByRole("alert");
  expect(screen.getByLabelText("Title for Edited Moon")).toHaveValue(
    "Edited Moon",
  );
  expect(service.confirm).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Confirm outline" }));
  await waitFor(() =>
    expect(handoff).toHaveBeenCalledWith(
      expect.objectContaining({
        draft_id: "draft",
        revision: 3,
        outline: savedDraft.outline,
      }),
    ),
  );
  expect(service.confirm).toHaveBeenCalledWith(savedDraft);
});

it("requires explicit branch recovery and retains both stored copies", async () => {
  const service = fakeService();
  const current = {
    ...savedDraft,
    revision: 4,
    outline: [{ ...outline[0], title: "Other device" }],
  };
  const recovery = {
    ...savedDraft,
    id: "recovery",
    revision: 1,
    outline: [{ ...outline[0], title: "My changes" }],
  };
  vi.mocked(service.save).mockRejectedValueOnce(
    new AuthError("Conflict", 409, { current, recovery }),
  );
  const selected = { ...recovery, revision: 2 };
  vi.mocked(service.save).mockResolvedValueOnce(selected);
  render(
    <CourseOutline
      briefRevision="brief"
      initialDraft={savedDraft}
      initialValue={savedDraft.outline}
      onBack={vi.fn()}
      service={service}
    />,
  );
  fireEvent.change(screen.getByLabelText("Title for Moon"), {
    target: { value: "My changes" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save outline" }));
  await screen.findByRole("alert");
  expect(
    screen.getByRole("button", { name: "Confirm outline" }),
  ).toBeDisabled();
  fireEvent.click(
    screen.getByRole("button", { name: "Continue my recovery copy" }),
  );
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Confirm outline" }),
    ).toBeEnabled(),
  );
  expect(screen.getByLabelText("Title for My changes")).toHaveValue(
    "My changes",
  );
  fireEvent.click(screen.getByRole("button", { name: "Confirm outline" }));
  await waitFor(() => expect(service.confirm).toHaveBeenCalledWith(selected));
  expect(service.save).toHaveBeenCalledTimes(2);
});
