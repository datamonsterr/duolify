import { expect, it } from "vitest";
import { sampleOutline } from "./outline";

// The API decodes outline drafts with DisallowUnknownFields
// (services/api/internal/courses/outline_editor.go:126), so a preview's extra lesson fields
// (lesson_id) must not reach POST /v1/outline-drafts.
it("keeps only the outline draft fields from a preview sample outline", () => {
  const units = [
    {
      title: "Sample unit",
      lessons: [
        { lesson_id: "lesson-1", title: "Sample lesson", goal: "Learn" },
      ],
    },
  ];
  const [unit] = sampleOutline(units);
  expect(Object.keys(unit).sort()).toEqual(["id", "lessons", "title"]);
  expect(Object.keys(unit.lessons[0]).sort()).toEqual(["goal", "id", "title"]);
  expect(unit.lessons[0]).toMatchObject({
    title: "Sample lesson",
    goal: "Learn",
  });
});
