import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ExampleCourses } from "./ExampleCourses";

const mocks = vi.hoisted(() => ({
  exampleCourses: vi.fn(),
  enrollExample: vi.fn(),
  navigateTo: vi.fn(),
  viewed: vi.fn(),
}));
vi.mock("../../lib/api", () => ({
  api: {
    exampleCourses: mocks.exampleCourses,
    enrollExample: mocks.enrollExample,
  },
}));
vi.mock("../../lib/navigation", () => ({ navigateTo: mocks.navigateTo }));
vi.mock("../../lib/analytics", () => ({
  trackExampleCoursesViewed: mocks.viewed,
}));
vi.mock("./localization/Preferences", () => ({
  useUiPreferences: () => ({ t: (key: string) => key }),
}));

const examples = [
  {
    slug: "product-management-foundations",
    title: "Product Management Foundations",
    description: "What a product manager does.",
    level: "beginner",
    units: 2,
    lessons: 6,
    estimated_minutes: 25,
  },
  {
    slug: "javascript-essentials",
    title: "JavaScript Essentials",
    description: "Values to loops.",
    level: "beginner",
    units: 2,
    lessons: 6,
    estimated_minutes: 25,
  },
];

function renderSection(props: Parameters<typeof ExampleCourses>[0]) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <ExampleCourses {...props} />
    </QueryClientProvider>,
  );
}

afterEach(() => vi.clearAllMocks());

describe("Start with an example", () => {
  it("lists the free examples, reports the view once and enrolls through the router", async () => {
    mocks.exampleCourses.mockResolvedValue(examples);
    mocks.enrollExample.mockResolvedValue({ course_id: "course-1" });
    renderSection({ surface: "learn_empty" });
    expect(await screen.findByText("Start with an example")).toBeTruthy();
    expect(screen.getAllByText("6 lessons · about 25 min · Free")).toHaveLength(
      2,
    );
    expect(screen.getAllByText("Example")).toHaveLength(2);
    expect(mocks.viewed).toHaveBeenCalledTimes(1);
    expect(mocks.viewed).toHaveBeenCalledWith("learn_empty");
    fireEvent.click(
      screen.getByRole("button", { name: "Start JavaScript Essentials" }),
    );
    await waitFor(() =>
      expect(mocks.navigateTo).toHaveBeenCalledWith("/course/course-1"),
    );
    expect(mocks.enrollExample).toHaveBeenCalledWith(
      "javascript-essentials",
      "learn_empty",
    );
  });

  it("does not offer an example the learner already has", async () => {
    mocks.exampleCourses.mockResolvedValue(examples);
    renderSection({
      surface: "courses",
      enrolledSlugs: ["javascript-essentials"],
    });
    expect(
      await screen.findByText("Product Management Foundations"),
    ).toBeTruthy();
    expect(screen.queryByText("JavaScript Essentials")).toBeNull();
  });

  it("renders nothing when every example is enrolled and says so when enrolling fails", async () => {
    mocks.exampleCourses.mockResolvedValue(examples);
    const { container } = renderSection({
      surface: "courses",
      enrolledSlugs: examples.map((e) => e.slug),
    });
    await waitFor(() => expect(mocks.exampleCourses).toHaveBeenCalled());
    expect(container.textContent).toBe("");
    expect(mocks.viewed).not.toHaveBeenCalled();
  });

  it("shows an error and stays on the page when enrollment fails", async () => {
    mocks.exampleCourses.mockResolvedValue(examples);
    mocks.enrollExample.mockRejectedValue(new Error("offline"));
    renderSection({ surface: "courses" });
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Start Product Management Foundations",
      }),
    );
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(mocks.navigateTo).not.toHaveBeenCalled();
  });
});
