import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { session } from "../accounts/session";
import { LessonTest } from "./LessonTest";

it("keeps an optional teach-back draft on failure and can show a self-check summary", async () => {
  const request = vi.spyOn(session, "request");
  const path = "/v1/courses/course/lessons/lesson/teach-back";
  request.mockImplementation(async (endpoint, method) => {
    if (endpoint !== path) return {};
    if (!method) return { turn: 0 };
    const posts = request.mock.calls.filter(
      ([url, verb]) => url === path && verb === "POST",
    );
    if (posts.length === 1) throw new TypeError("Failed to fetch");
    return {
      turn: 3,
      summary: {
        selfCheck: true,
        covered: [],
        missed: [{ text: "Moonrise", slideIndex: 2 }],
      },
    };
  });

  render(
    <LessonTest
      courseId="course"
      lessonId="lesson"
      questions={[]}
      passed
      onPass={vi.fn()}
    />,
  );
  expect(request.mock.calls.some(([url]) => url === path)).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: "Teach it back" }));
  const explanation = await screen.findByRole("textbox", {
    name: "Your explanation",
  });
  expect(
    screen.getByRole("link", { name: /back to your path/i }),
  ).toBeVisible();
  fireEvent.change(explanation, { target: { value: "  The Moon moves.  " } });
  const send = screen.getByRole("button", { name: "Send explanation" });
  fireEvent.click(send);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Could not send your explanation.",
  );
  expect(explanation).toHaveValue("  The Moon moves.  ");
  fireEvent.click(send);
  expect(await screen.findByText("Check your explanation")).toBeVisible();
  expect(screen.queryByText("Missed")).not.toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Moonrise" })).toHaveAttribute(
    "href",
    "/lesson/lesson?slide=2&t=0",
  );
  expect(request).toHaveBeenCalledWith(path, "POST", {
    turn: 0,
    explanation: "The Moon moves.",
  });
  request.mockRestore();
});
