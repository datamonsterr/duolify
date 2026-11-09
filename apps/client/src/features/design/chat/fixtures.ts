import type { Outline } from "./OutlineEditor";
import type { OutlineProposal } from "./ProposalDiff";
import type { SummaryLine } from "./LiveSummary";
export const initialSummary: SummaryLine[] = [
  {
    id: "topic",
    labelId: "summary.topic",
    label: "Topic",
    value: "Astronomy for curious beginners",
    required: true,
  },
  {
    id: "goal",
    labelId: "summary.goal",
    label: "Goal",
    value: "Explain the night sky to my kids",
  },
  {
    id: "include",
    labelId: "summary.include",
    label: "Include",
    value: "Constellations · Moon phases · Planets",
  },
  {
    id: "skip",
    labelId: "summary.skip",
    label: "Skip topics",
    value: "History of astronomy",
  },
  { id: "viewing", labelId: "summary.viewing", label: "Viewing", value: "" },
  {
    id: "lessons",
    labelId: "summary.lessons",
    label: "Lessons",
    value: "About 6 min · English",
  },
];
export const initialOutline: Outline = [
  {
    id: "bearings",
    title: "Finding your bearings",
    lessons: [
      {
        id: "map",
        title: "A map of the night",
        goal: "Name the main parts of the night sky",
      },
      {
        id: "sky",
        title: "Why the sky changes",
        goal: "Explain why stars appear to move",
      },
    ],
  },
  {
    id: "moon",
    title: "The Moon",
    lessons: [
      {
        id: "phases",
        title: "Moon phases",
        goal: "Explain the changing shape of the Moon",
      },
    ],
  },
  {
    id: "planets",
    title: "Planets",
    lessons: [
      { id: "binoculars", title: "Planets in view" },
      { id: "history", title: "History of astronomy" },
    ],
  },
];
export const fixtureProposal: OutlineProposal = {
  id: "fixture-proposal",
  changes: [
    {
      id: "a",
      kind: "added",
      description: "Your first telescope · one lesson",
    },
    { id: "m", kind: "moved", description: "Planets before the Moon" },
    { id: "r", kind: "removed", description: "History of astronomy" },
    {
      id: "t",
      kind: "retitled",
      description: "Planets in view → Planets through binoculars",
    },
  ],
};
export function applyFixtureProposal(value: Outline): Outline {
  const planets = value.find((unit) => unit.id === "planets");
  const other = value.filter(
    (unit) => unit.id !== "planets" && unit.id !== "telescope",
  );
  if (planets)
    other.splice(Math.min(1, other.length), 0, {
      ...planets,
      lessons: planets.lessons
        .filter((lesson) => lesson.id !== "history")
        .map((lesson) =>
          lesson.id === "binoculars"
            ? { ...lesson, title: "Planets through binoculars" }
            : lesson,
        ),
    });
  return [
    ...other,
    {
      id: "telescope",
      title: "Your first telescope",
      lessons: [{ id: "choose", title: "Choosing a first telescope" }],
    },
  ];
}
