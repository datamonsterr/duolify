import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
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

it("announces loading and offers recovery without exposing server details", async () => {
  let done!: (v: typeof ready) => void;
  request.mockReturnValueOnce(new Promise((resolve) => (done = resolve)));
  render(<CourseReviews courseId="course" />);
  expect(screen.getByRole("status")).toHaveTextContent("Loading reviews");
  await act(async () => done(ready));
  expect(screen.getByTestId("course-reviews")).toHaveTextContent(
    "No reviews yet",
  );
});
it("accepts blank text, counts Unicode characters and refuses oversized drafts", async () => {
  request.mockResolvedValueOnce(ready);
  render(<CourseReviews courseId="course" />);
  fireEvent.click(await screen.findByLabelText("1 star"));
  const box = screen.getByLabelText("Review text");
  fireEvent.change(box, { target: { value: "界".repeat(2001) } });
  expect(screen.getByRole("button", { name: "Post review" })).toBeDisabled();
  fireEvent.change(box, { target: { value: "界".repeat(2000) } });
  expect(screen.getByRole("button", { name: "Post review" })).toBeEnabled();
  fireEvent.change(box, { target: { value: "" } });
  request
    .mockResolvedValueOnce({ verdict: "pass", text: "", stars: 1 })
    .mockResolvedValueOnce({
      ...ready,
      mine: {
        id: "r",
        stars: 1,
        version: 1,
        verdict: "pass",
        text: "",
        author: "Learner",
      },
    });
  fireEvent.click(screen.getByRole("button", { name: "Post review" }));
  expect(await screen.findByTestId("review-status")).toHaveTextContent(
    "Review saved",
  );
  expect(request).toHaveBeenCalledWith(
    "/v1/marketplace/courses/course/review",
    "PUT",
    { stars: 1, text: "" },
  );
});
it("refuses owners and preserves the displayed reviewed version", async () => {
  request.mockResolvedValueOnce({
    ...ready,
    owner: true,
    can_review: false,
    mine: null,
  });
  render(<CourseReviews courseId="course" />);
  expect(await screen.findByTestId("review-gate")).toHaveTextContent(
    "own course",
  );
  expect(screen.queryByLabelText("Review text")).toBeNull();
});
it("confirms deletion and updates the aggregate after removing one review", async () => {
  const mine = {
    id: "r",
    stars: 4,
    version: 2,
    verdict: "pass",
    text: "Helpful",
    author: "Learner",
  };
  request.mockResolvedValueOnce({
    ...ready,
    mine,
    items: [mine],
    rating_count: 1,
    rating_average: 4,
  });
  const changed = vi.fn();
  render(<CourseReviews courseId="course" onRatingChange={changed} />);
  fireEvent.click(await screen.findByRole("button", { name: "Delete review" }));
  expect(screen.getByRole("dialog")).toHaveTextContent("removes your stars");
  expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus();
  request.mockResolvedValueOnce(undefined).mockResolvedValueOnce(ready);
  fireEvent.click(screen.getByRole("button", { name: "Confirm delete" }));
  await waitFor(() => expect(changed).toHaveBeenCalledWith(0, null));
  expect(screen.getByTestId("review-status")).toHaveTextContent(
    "Review deleted",
  );
  expect(request).toHaveBeenCalledWith(
    "/v1/marketplace/courses/course/review",
    "DELETE",
  );
});
