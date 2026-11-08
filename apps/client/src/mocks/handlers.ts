import { http, HttpResponse } from "msw";
import type { Course } from "../lib/api";
export const courses: Course[] = [
  {
    id: "astronomy",
    title: "Everyday astronomy",
    description: "Find your way around the night sky.",
    units: [
      {
        title: "Unit 1 · Meet the sky",
        lessons: [
          { id: "welcome", title: "Begin exploring", state: "done" },
          { id: "sky", title: "Look up", state: "current" },
          { id: "stars", title: "Meet the stars", state: "locked" },
          { id: "moon", title: "Our moon", state: "locked" },
        ],
      },
      {
        title: "Unit 2 · Our solar system",
        lessons: [{ id: "planets", title: "The planets", state: "locked" }],
      },
    ],
  },
  {
    id: "plants",
    title: "Plant science",
    description: "Grow your understanding of the living world.",
    units: [
      {
        title: "Unit 1 · Getting started",
        lessons: [{ id: "roots", title: "Roots and shoots", state: "current" }],
      },
    ],
  },
];
export const handlers = [
  http.get("*/courses", () => HttpResponse.json(courses)),
  http.get("*/courses/:id", ({ params }) => {
    const course = courses.find((c) => c.id === params.id);
    return course
      ? HttpResponse.json(course)
      : new HttpResponse(null, { status: 404 });
  }),
];
