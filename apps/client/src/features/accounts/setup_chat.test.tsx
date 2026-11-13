import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { SetupChat } from "./SetupChat";
import { emptyContext } from "./learner-context";
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

it("handles single follow-up, skipped fields, summary validation, bounded failures, and one assistant announcement", async () => {
  const commit = vi.fn().mockResolvedValue({
    ...emptyContext(),
    topic: "Astronomy",
    setup_completed: true,
  });

  const chatApi = vi.fn();
  // Turn 1: vague answer -> single follow-up
  chatApi.mockResolvedValueOnce({
    message: "Space is a great pick! Is this for fun, for school, or for work?",
    quick_replies: [
      { id: "fun", label: "For fun" },
      { id: "school", label: "For school" },
      { id: "work", label: "For work" },
      { id: "skip", label: "Skip" },
    ],
    updated_profile: { topic: "Astronomy", goal: "Something about space" },
    next_step: "goal",
    is_follow_up: true,
    is_complete: false,
  });

  // Turn 2: answer to follow-up -> moves to next step (level) without 2nd follow-up
  chatApi.mockResolvedValueOnce({
    message: "What level would you describe yourself as?",
    quick_replies: [
      { id: "beginner", label: "Beginner" },
      { id: "intermediate", label: "Intermediate" },
      { id: "skip", label: "Skip" },
    ],
    updated_profile: { topic: "Astronomy", goal: "Fun with kids", level: "" },
    next_step: "level",
    is_follow_up: false,
    is_complete: false,
  });

  render(
    <SetupChat
      profile={emptyContext()}
      onCommit={commit}
      chatClient={chatApi}
    />,
  );

  // 1. Initial question announced
  expect(
    screen.getByText("What would you like to get better at?"),
  ).toBeInTheDocument();

  // Assistant announcement test: live log has the message exactly once
  const log = screen.getByRole("log", { name: /new messages/i });
  expect(log.textContent).toContain("What would you like to get better at?");

  // 2. Submit vague answer
  const input = screen.getByRole("textbox", { name: /your answer/i });
  fireEvent.change(input, { target: { value: "Something about space" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  await waitFor(() => {
    expect(
      screen.getByText(
        "Space is a great pick! Is this for fun, for school, or for work?",
      ),
    ).toBeInTheDocument();
  });

  // Announcement count: follow-up message announced once
  await waitFor(() => {
    expect(log.textContent?.split("Space is a great pick!").length - 1).toBe(1);
  });

  // 3. Single follow-up bound: answering follow-up advances to next question without second follow-up
  fireEvent.change(input, { target: { value: "For fun" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  await waitFor(() => {
    expect(
      screen.getByText("What level would you describe yourself as?"),
    ).toBeInTheDocument();
  });

  // 4. Skipped fields: clicking Skip advances without inventing a level
  chatApi.mockResolvedValueOnce({
    message: "How much time can you give it on a normal day?",
    quick_replies: [
      { id: "10", label: "10 min" },
      { id: "skip", label: "Skip" },
    ],
    updated_profile: { topic: "Astronomy", level: "" },
    next_step: "daily_time",
    is_follow_up: false,
    is_complete: false,
  });

  fireEvent.click(screen.getByRole("button", { name: /^skip$/i }));

  await waitFor(() => {
    expect(
      screen.getByText("How much time can you give it on a normal day?"),
    ).toBeInTheDocument();
  });

  // 5. Summary validation: live summary shows current extracted topic
  expect(screen.getByText("Astronomy")).toBeInTheDocument();

  // 6. Bounded failure: provider error opens form fallback with answers intact
  chatApi.mockRejectedValueOnce(new Error("Assistant unavailable"));
  fireEvent.click(screen.getByRole("button", { name: "10 min" }));

  await waitFor(() => {
    expect(
      screen.getByText("The setup chat isn't responding"),
    ).toBeInTheDocument();
  });

  // Form fallback has the answers kept intact
  expect(screen.getByLabelText("Topic", { exact: true })).toHaveValue(
    "Astronomy",
  );
  expect(
    screen.getByRole("button", { name: "Try chat again" }),
  ).toBeInTheDocument();
});
