import { http, HttpResponse } from "msw";
import { playerManifest, playerHTML } from "./player";
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
  http.get("*/v1/lessons/:lesson/position", () => HttpResponse.json(null)),
  http.put("*/v1/lessons/:lesson/position", async ({ request }) =>
    HttpResponse.json(await request.json()),
  ),
  http.get("*/v1/lessons/:lesson/manifest", ({ params }) =>
    HttpResponse.json(playerManifest(String(params.lesson))),
  ),
  http.get("*/v1/lessons/:lesson/slides/:slide", () =>
    HttpResponse.json({ html: playerHTML }),
  ),
  http.get("*/v1/courses", () =>
    HttpResponse.json({ courses, active_course_id: courses[0]?.id }),
  ),
  http.post("*/v1/courses/:id/select", ({ params }) =>
    HttpResponse.json({ active_course_id: params.id }),
  ),
  http.post("*/v1/courses/:id/level", async ({ request }) => {
    const body = (await request.json().catch(() => ({}))) as { level?: string };
    return HttpResponse.json({ level_override: body.level ?? "" });
  }),
  http.get("*/courses", () => HttpResponse.json(courses)),
  http.get("*/courses/:id", ({ params }) => {
    const course = courses.find((c) => c.id === params.id);
    return course
      ? HttpResponse.json(course)
      : new HttpResponse(null, { status: 404 });
  }),
  http.get("*/v1/features", () =>
    HttpResponse.json({ lesson_test_tracer_enabled: true }),
  ),
];
