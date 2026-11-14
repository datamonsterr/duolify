import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { CourseReviews } from "./CourseReviews";
import { session } from "../accounts/session";
vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));
const request = vi.mocked(session.request);
beforeEach(() => request.mockReset());
const ready = {
  items: [],
  mine: null,
  lessons_needed: 0,
  completed_lessons: 3,
  min_lessons: 3,
  owner: false,
  can_review: true,
  rating_count: 0,
  rating_average: null,
};
it("shows remaining lessons and no form before the gate", async () => {
  request.mockResolvedValue({
    ...ready,
    can_review: false,
    lessons_needed: 2,
    completed_lessons: 1,
  });
  render(<CourseReviews courseId="course" />);
  expect(await screen.findByTestId("review-gate")).toHaveTextContent("2 more");
  expect(
    screen.queryByRole("button", { name: "Post review" }),
  ).not.toBeInTheDocument();
});
it("submits stars and text, hides blocked text and retains draft after failure", async () => {
  request
    .mockResolvedValueOnce(ready)
    .mockRejectedValueOnce(new TypeError("offline"));
  render(<CourseReviews courseId="course" />);
  fireEvent.click(await screen.findByLabelText("5 stars"));
  fireEvent.change(screen.getByLabelText("Review text"), {
    target: { value: "Helpful" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Post review" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't reach");
  expect(screen.getByLabelText("Review text")).toHaveValue("Helpful");
  request
    .mockResolvedValueOnce({ verdict: "block", stars: 5 })
    .mockResolvedValueOnce({
      ...ready,
      mine: { id: "r", stars: 5, text: "", version: 1, verdict: "block" },
      items: [
        {
          id: "r",
          stars: 5,
          text: "",
          version: 1,
          verdict: "block",
          author: "Learner",
        },
      ],
    });
  fireEvent.click(screen.getByRole("button", { name: "Post review" }));
  await waitFor(() =>
    expect(screen.getByTestId("review-status")).toHaveTextContent(
      "text is hidden",
    ),
  );
  expect(request).toHaveBeenCalledWith(
    "/v1/marketplace/courses/course/review",
    "PUT",
    { stars: 5, text: "Helpful" },
  );
});
