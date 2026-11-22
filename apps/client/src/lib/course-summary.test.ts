import { describe, expect, it } from "vitest";
import { summarizeCourse, type Course } from "./api";

const lesson = (
  id: string,
  state: Course["units"][0]["lessons"][0]["state"],
) => ({
  id,
  title: id,
  state,
});

describe("summarizeCourse", () => {
  it("reduces a full tree to counts and the current position", () => {
    const course: Course = {
      id: "c1",
      title: "Astronomy",
      description: "Sky",
      units: [
        { title: "A", lessons: [lesson("1", "done"), lesson("2", "done")] },
        {
          title: "B",
          lessons: [
            lesson("3", "done"),
            lesson("4", "current"),
            lesson("5", "locked"),
          ],
        },
      ],
    };
    const summary = summarizeCourse(course);
    expect(summary).not.toHaveProperty("units");
    expect(summary.progress).toEqual({
      lessons_total: 5,
      lessons_done: 3,
      current: { unit_index: 1, lesson_index: 1, unit_lessons: 3 },
    });
  });

  it("reports no current lesson when everything is done", () => {
    const summary = summarizeCourse({
      id: "c2",
      title: "Done",
      description: "",
      units: [{ title: "A", lessons: [lesson("1", "done")] }],
    });
    expect(summary.progress).toEqual({
      lessons_total: 1,
      lessons_done: 1,
      current: null,
    });
  });

  it("passes an API summary through untouched", () => {
    const api = {
      id: "c3",
      title: "T",
      description: "",
      progress: { lessons_total: 4, lessons_done: 1, current: null },
    };
    expect(summarizeCourse(api)).toEqual(api);
  });
});
