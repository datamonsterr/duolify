import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { LearnerContextEditor } from "./LearnerContextEditor";
import { emptyContext, contextFields } from "./learner-context";
import { configureAnalytics } from "../../lib/analytics";
const posthog = vi.hoisted(() => ({
  capture: vi.fn(),
  has_opted_out_capturing: vi.fn(() => false),
  identify: vi.fn(),
  init: vi.fn(),
}));
vi.mock("posthog-js/dist/module.no-external", () => ({ default: posthog }));
afterEach(() => {
  configureAnalytics(null);
  vi.clearAllMocks();
});
it("hands every collected chat answer to an editable fallback and confirms only explicitly", async () => {
  const answers = {
    goal: "Teach my kids",
    topic: "Astronomy",
    background: "School physics",
    level: "beginner",
    daily_minutes: 10,
    daily_xp: 20,
    reminder_enabled: false,
    reminder_time: "09:30",
    locale: "vi-VN",
    time_zone: "Asia/Ho_Chi_Minh",
  };
  const save = vi.fn().mockResolvedValue({
    ...emptyContext(),
    ...answers,
    setup_completed: true,
  });
  render(
    <LearnerContextEditor
      profile={emptyContext()}
      setup
      initialAnswers={answers}
      onCommit={save}
    />,
  );
  expect(screen.getByText("AI uses your optional answers")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Use form instead" }));
  for (const field of contextFields)
    expect(
      screen.getByLabelText(field.label, { exact: true }),
    ).toBeInTheDocument();
  expect(screen.getByLabelText("Goal", { exact: true })).toHaveValue(
    answers.goal,
  );
  expect(screen.getByLabelText("Reminder", { exact: true })).toHaveValue(
    "false",
  );
  fireEvent.change(screen.getByLabelText("Background", { exact: true }), {
    target: { value: "" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Review summary" }));
  expect(save).not.toHaveBeenCalled();
  for (const field of contextFields)
    expect(
      screen.getByLabelText(field.label, { exact: true }),
    ).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Confirm profile" }));
  await waitFor(() =>
    expect(save).toHaveBeenCalledWith("save", { ...answers, background: "" }),
  );
  expect(await screen.findByText("Setup complete")).toBeVisible();
  // Only the API owns committed domain analytics.
  expect(posthog.capture).not.toHaveBeenCalledWith(
    "learner_profile_updated",
    expect.anything(),
  );
  expect(posthog.capture).not.toHaveBeenCalledWith(
    "onboarding_completed",
    expect.anything(),
  );
});
it("retains private drafts on failure, clears optional fields, and skips without sending draft values", async () => {
  const save = vi
    .fn()
    .mockRejectedValueOnce(new Error("Could not save profile. Retry."))
    .mockResolvedValue(emptyContext());
  render(
    <LearnerContextEditor
      profile={emptyContext()}
      setup
      initialAnswers={{ goal: "Private draft" }}
      onCommit={save}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Use form instead" }));
  fireEvent.click(screen.getByRole("button", { name: "Review summary" }));
  fireEvent.click(screen.getByRole("button", { name: "Confirm profile" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not save");
  expect(screen.getByLabelText("Goal", { exact: true })).toHaveValue(
    "Private draft",
  );
  fireEvent.click(screen.getByRole("button", { name: "Skip setup" }));
  await waitFor(() => expect(save).toHaveBeenLastCalledWith("skip", {}));
});
it("uses configured Unicode limits, shows errors without losing edits, and leaves skipped goals empty", async () => {
  const save = vi.fn();
  render(
    <LearnerContextEditor
      profile={{ ...emptyContext(), text_limit: 3 }}
      onCommit={save}
    />,
  );
  expect(screen.getByLabelText("Daily minutes", { exact: true })).toHaveValue(
    null,
  );
  fireEvent.change(screen.getByLabelText("Goal", { exact: true }), {
    target: { value: "界界界界" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Check the highlighted fields",
  );
  expect(screen.getByLabelText("Goal", { exact: true })).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  expect(save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Clear profile" }));
  expect(screen.getByLabelText("Goal", { exact: true })).toHaveValue("");
  expect(screen.getByLabelText("Daily minutes", { exact: true })).toHaveValue(
    null,
  );
});
