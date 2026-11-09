import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { GuidedChat, type ChatMessage } from "./chat/GuidedChat";
import { OutlineEditor } from "./chat/OutlineEditor";
import { ProposalDiff } from "./chat/ProposalDiff";
import { initialOutline, fixtureProposal } from "./chat/fixtures";
import { configureUiAnalytics, trackGuidedUi } from "../../lib/analytics";
import { Sheet } from "./sheet/Sheet";
import { StrictMode } from "react";

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

it("announces completed assistant messages once, never tokens, and keeps the composer", () => {
  const props = {
    messages: [] as ChatMessage[],
    question: { id: "view", replies: [{ id: "city", label: "City balcony" }] },
    draft: "Draft",
    onDraftChange: vi.fn(),
    onAnswer: vi.fn(),
    formHref: "#form",
  };
  const { rerender } = render(<GuidedChat {...props} />);
  const composer = screen.getByRole("textbox", { name: "Your answer" });
  composer.focus();
  const stream: ChatMessage = {
    id: "a1",
    author: "assistant",
    authorLabel: "Duolify",
    text: "Some",
    status: "streaming",
  };
  rerender(<GuidedChat {...props} messages={[stream]} />);
  const log = screen.getByRole("log", { name: "New assistant messages" });
  expect(log).toBeEmptyDOMElement();
  rerender(
    <GuidedChat
      {...props}
      messages={[{ ...stream, text: "Some more tokens" }]}
    />,
  );
  expect(log).toBeEmptyDOMElement();
  const completed = {
    ...stream,
    text: "One completed question?",
    status: "complete" as const,
  };
  rerender(<GuidedChat {...props} messages={[completed]} />);
  expect(
    within(log).getAllByText("Duolify: One completed question?"),
  ).toHaveLength(1);
  rerender(<GuidedChat {...props} messages={[{ ...completed }]} />);
  expect(
    within(log).getAllByText("Duolify: One completed question?"),
  ).toHaveLength(1);
  expect(composer).toHaveFocus();
  fireEvent.click(screen.getByRole("button", { name: "City balcony" }));
  expect(screen.getByRole("button", { name: "City balcony" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  fireEvent.click(screen.getByRole("button", { name: "Send answer" }));
  expect(props.onAnswer).toHaveBeenLastCalledWith({
    text: "Draft",
    replyIds: ["city"],
    skipped: false,
  });
  expect(composer).toHaveFocus();
  fireEvent.click(screen.getByRole("button", { name: "Skip" }));
  expect(props.onAnswer).toHaveBeenLastCalledWith({
    text: "",
    replyIds: [],
    skipped: true,
  });
});

it("moves units and lessons with retained focus; removes, adds, renames and undoes", () => {
  const changed = vi.fn();
  render(<OutlineEditor initialValue={initialOutline} onChange={changed} />);
  const move = screen.getByRole("button", { name: "Move Planets up" });
  move.focus();
  fireEvent.click(move);
  expect(move).toHaveFocus();
  expect(screen.getByRole("status")).toHaveTextContent(
    "Planets moved to position 2",
  );
  expect(
    changed.mock.lastCall![0].map((unit: { id: string }) => unit.id),
  ).toEqual(["bearings", "planets", "moon"]);
  const lessonMove = screen.getByRole("button", {
    name: "Move Why the sky changes up",
  });
  lessonMove.focus();
  fireEvent.click(lessonMove);
  expect(lessonMove).toHaveFocus();
  expect(screen.getByRole("status")).toHaveTextContent(
    "Why the sky changes moved to position 1",
  );
  fireEvent.change(screen.getByRole("textbox", { name: "Title for Planets" }), {
    target: { value: "Nearby planets" },
  });
  expect(
    screen.getByRole("textbox", { name: "Title for Nearby planets" }),
  ).toHaveValue("Nearby planets");
  fireEvent.click(
    screen.getByRole("button", { name: "Remove Nearby planets" }),
  );
  expect(
    screen.queryByRole("textbox", { name: "Title for Nearby planets" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Undo" })).toHaveFocus();
  fireEvent.click(screen.getByRole("button", { name: "Undo" }));
  expect(
    screen.getByRole("textbox", { name: "Title for Nearby planets" }),
  ).toHaveFocus();
  fireEvent.click(
    screen.getByRole("button", { name: "Add a lesson to Nearby planets" }),
  );
  expect(
    screen.getByRole("textbox", { name: "Title for New lesson" }),
  ).toHaveFocus();
  fireEvent.click(screen.getByRole("button", { name: "Add a unit" }));
  expect(
    screen.getByRole("textbox", { name: "Title for New unit" }),
  ).toHaveFocus();
  fireEvent.change(
    screen.getByRole("textbox", { name: "Title for New unit" }),
    { target: { value: "" } },
  );
  expect(
    screen.getByRole("textbox", { name: "Title for Untitled unit" }),
  ).toHaveAttribute("aria-invalid", "true");
});

it("supports handle keyboard reorder and drag reorder while refusing cross-list drops", () => {
  const changed = vi.fn();
  render(<OutlineEditor initialValue={initialOutline} onChange={changed} />);
  const handle = screen.getByRole("button", { name: "Reorder Planets" });
  handle.focus();
  fireEvent.keyDown(handle, { key: "ArrowUp" });
  expect(handle).toHaveFocus();
  expect(changed.mock.lastCall![0][1].id).toBe("planets");
  const transfer = {
    setData: vi.fn(),
    getData: vi.fn(() => "planets"),
    effectAllowed: "move",
  };
  fireEvent.dragStart(handle, { dataTransfer: transfer });
  fireEvent.drop(
    screen.getByRole("button", { name: "Reorder Finding your bearings" }),
    { dataTransfer: transfer },
  );
  expect(changed.mock.lastCall![0][0].id).toBe("planets");
  const count = changed.mock.calls.length;
  fireEvent.dragStart(handle, { dataTransfer: transfer });
  fireEvent.drop(
    screen.getByRole("button", { name: "Reorder A map of the night" }),
    { dataTransfer: transfer },
  );
  expect(changed).toHaveBeenCalledTimes(count);
});

it("labels every proposal operation and separates Apply from Discard", () => {
  const apply = vi.fn(),
    discard = vi.fn();
  render(
    <ProposalDiff
      proposal={fixtureProposal}
      onApply={apply}
      onDiscard={discard}
    />,
  );
  for (const label of ["Added", "Removed", "Moved", "Retitled"])
    expect(screen.getByText(label, { exact: true })).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Discard" }));
  expect(discard).toHaveBeenCalledOnce();
  expect(apply).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Apply" }));
  expect(apply).toHaveBeenCalledOnce();
});

it("delivers catalogued controls and sheet events with UUID/common properties and no text", () => {
  const context = {
    learnerId: "56d4e614-7665-4f14-b83e-8b5e9bed384a",
    platform: "web" as const,
    appVersion: "0.1.0",
    enabled: true,
  };
  configureUiAnalytics(context);
  const fixture = render(
    <StrictMode>
      <Sheet
        open
        title="Private course title"
        sheetId="course_brief"
        onClose={vi.fn()}
      >
        <input aria-label="Private draft" defaultValue="Private answer" />
      </Sheet>
    </StrictMode>,
  );
  expect(posthog.capture).toHaveBeenCalledTimes(1);
  expect(posthog.capture).toHaveBeenLastCalledWith("ui_sheet_opened", {
    distinct_id: context.learnerId,
    platform: "web",
    app_version: "0.1.0",
    source: "client",
    sheet_id: "course_brief",
  });
  fixture.unmount();
  render(
    <GuidedChat
      messages={[]}
      question={{
        id: "private question",
        replies: [{ id: "private id", label: "Private learner topic" }],
      }}
      draft="private answer"
      onDraftChange={vi.fn()}
      onAnswer={vi.fn()}
      formHref="#form"
    />,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Private learner topic" }),
  );
  expect(posthog.capture).toHaveBeenLastCalledWith("ui_control_activated", {
    distinct_id: context.learnerId,
    platform: "web",
    app_version: "0.1.0",
    source: "client",
    surface: "guided_chat",
    control_id: "quick_reply",
  });
  expect(JSON.stringify(posthog.capture.mock.calls)).not.toMatch(/private/i);
  posthog.capture.mockClear();
  // Runtime allowlists also protect JavaScript callers and hostile extra properties.
  trackGuidedUi({
    event: "ui_control_activated",
    surface: "guided_chat",
    controlId: "private text",
  } as never);
  trackGuidedUi({ event: "ui_sheet_opened", sheetId: "private text" } as never);
  expect(posthog.capture).not.toHaveBeenCalled();
  configureUiAnalytics({ ...context, enabled: false });
  trackGuidedUi({ event: "ui_sheet_opened", sheetId: "course_brief" });
  configureUiAnalytics({ ...context, learnerId: "email@example.com" });
  trackGuidedUi({ event: "ui_sheet_opened", sheetId: "course_brief" });
  configureUiAnalytics(null);
  trackGuidedUi({ event: "ui_sheet_opened", sheetId: "course_brief" });
  expect(posthog.capture).not.toHaveBeenCalled();
  configureUiAnalytics(context);
  posthog.has_opted_out_capturing.mockReturnValueOnce(true);
  trackGuidedUi({ event: "ui_sheet_opened", sheetId: "course_brief" });
  expect(posthog.capture).not.toHaveBeenCalled();
  posthog.capture.mockImplementationOnce(() => {
    throw new Error("offline");
  });
  expect(() =>
    trackGuidedUi({ event: "ui_sheet_opened", sheetId: "course_brief" }),
  ).not.toThrow();
});
