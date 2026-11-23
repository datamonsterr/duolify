import { describe, expect, it } from "vitest";
import {
  answer,
  back,
  current,
  draftAnswers,
  edit,
  restore,
  skip,
  start,
  toDraft,
  walk,
} from "./engine";
import { evalExpr } from "./expr";
import { fixtureSecure, fixtureTour } from "./scripts";
import { validateStep } from "./validate";
import type { GuidedState } from "./engine";
import type { Value } from "./types";

function run(state: GuidedState, steps: [string, Value][]) {
  for (const [id, value] of steps) {
    const out = answer(fixtureTour, state, id, value);
    expect(out.error, id).toBeUndefined();
    state = out.state;
  }
  return state;
}
const base: [string, Value][] = [
  ["name", "Sam"],
  ["level", "some"],
  ["goals", ["speak"]],
];

describe("expression language", () => {
  const answers = {
    a: { value: "x" },
    b: { value: true },
    s: { value: null, skipped: true },
  };
  it("evaluates eq, in, answered, not", () => {
    expect(evalExpr({ eq: ["a", "x"] }, answers)).toBe(true);
    expect(evalExpr({ in: ["a", ["y", "x"]] }, answers)).toBe(true);
    expect(evalExpr({ answered: "b" }, answers)).toBe(true);
    expect(evalExpr({ answered: "s" }, answers)).toBe(false);
    expect(evalExpr({ not: { answered: "zz" } }, answers)).toBe(true);
  });
});

describe("validators", () => {
  const step = (id: string) => fixtureTour.steps.find((s) => s.id === id)!;
  it("checks time, date, number and options", () => {
    expect(validateStep(step("time"), "7:30")).toBe("invalid");
    expect(validateStep(step("time"), "07:30")).toBeNull();
    expect(validateStep(step("date"), "2026-02-31")).toBe("invalid");
    expect(validateStep(step("minutes"), 1441)).toBe("invalid");
    expect(validateStep(step("minutes"), 20)).toBeNull();
    expect(validateStep(step("level"), "nope")).toBe("invalid");
    expect(validateStep(step("name"), "  ")).toBe("required");
  });
  it("checks secure inputs", () => {
    const [email, code] = fixtureSecure.steps;
    expect(validateStep(email, "a@b")).toBe("invalid");
    expect(validateStep(email, "a@b.co")).toBeNull();
    expect(validateStep(code, "12345")).toBe("invalid");
    expect(validateStep(code, "123456")).toBeNull();
  });
});

describe("engine", () => {
  it("walks branches and asks the next unanswered step", () => {
    let state = run(start(fixtureTour), base);
    expect(current(fixtureTour, state)).toBe("remind");
    state = run(state, [["remind", true]]);
    expect(current(fixtureTour, state)).toBe("time");
    state = run(state, [["time", "07:30"]]);
    expect(current(fixtureTour, state)).toBe("date");
  });
  it("rejects invalid answers without changing state", () => {
    const state = start(fixtureTour);
    const out = answer(fixtureTour, state, "name", "");
    expect(out.error).toBe("required");
    expect(out.state).toBe(state);
  });
  it("keeps later answers after an edit that does not affect them", () => {
    const state = run(start(fixtureTour), [
      ...base,
      ["remind", true],
      ["time", "07:30"],
      ["date", "2026-12-01"],
    ]);
    const out = answer(fixtureTour, edit(state, "level"), "level", "expert");
    expect(out.dropped).toEqual([]);
    expect(out.state.answers.date.value).toBe("2026-12-01");
    expect(current(fixtureTour, out.state)).toBe("minutes");
  });
  it("drops answers made unreachable and says which", () => {
    const state = run(start(fixtureTour), [
      ...base,
      ["remind", true],
      ["time", "07:30"],
      ["date", "2026-12-01"],
    ]);
    const out = answer(fixtureTour, edit(state, "remind"), "remind", false);
    expect(out.dropped).toEqual(["time"]);
    expect(out.state.answers.time).toBeUndefined();
    expect(out.state.answers.date.value).toBe("2026-12-01");
  });
  it("requires a fresh confirm after any edit", () => {
    let state = run(start(fixtureTour), [
      ...base,
      ["remind", false],
      ["date", "2026-12-01"],
      ["minutes", 15],
      ["notes", "hi"],
      ["rating", 4],
    ]);
    state = skip(fixtureTour, state, "photo").state;
    state = run(state, [["confirm", true]]);
    expect(current(fixtureTour, state)).toBeNull();
    const out = answer(fixtureTour, state, "name", "Alex");
    expect(current(fixtureTour, out.state)).toBe("confirm");
  });
  it("skips only skippable steps", () => {
    const state = start(fixtureTour);
    expect(skip(fixtureTour, state, "name").error).toBe("invalid");
    const out = skip(fixtureTour, run(state, [["name", "Sam"]]), "level");
    expect(out.state.answers.level.skipped).toBe(true);
    expect(current(fixtureTour, out.state)).toBe("goals");
  });
  it("goes back to the last answer", () => {
    const state = run(start(fixtureTour), base);
    expect(back(fixtureTour, state).editing).toBe("goals");
  });
  it("never lets a path step repeat or loop", () => {
    expect(walk(fixtureTour, {}).path).toEqual([]);
  });
});

describe("drafts and resume", () => {
  it("leaves secrets and non-persistent steps out of the draft", () => {
    const answers = {
      email: { value: "a@b.co" },
      code: { value: "123456" },
      password: { value: "x" },
    };
    expect(draftAnswers(fixtureSecure, answers)).toEqual({});
    const photo = { photo: { value: "me.png" }, name: { value: "Sam" } };
    expect(Object.keys(draftAnswers(fixtureTour, photo))).toEqual(["name"]);
  });
  it("resumes at the first unanswered step", () => {
    const state = run(start(fixtureTour), base);
    const draft = toDraft(fixtureTour, state)!;
    const resumed = restore(fixtureTour, JSON.parse(JSON.stringify(draft)));
    expect(resumed.notice).toBeNull();
    expect(current(fixtureTour, resumed.state)).toBe("remind");
  });
  it("migrates matching answers when the version changed", () => {
    const state = run(start(fixtureTour), base);
    const draft = { ...toDraft(fixtureTour, state)!, script_version: 0 };
    draft.answers = {
      ...draft.answers,
      gone: { value: "x" },
      level: { value: "bogus" },
    };
    const out = restore(fixtureTour, draft);
    expect(out.notice).toBe("migrated");
    expect(Object.keys(out.state.answers).sort()).toEqual(["goals", "name"]);
  });
  it("restarts with a notice when nothing matches", () => {
    const out = restore(fixtureTour, {
      script_version: 0,
      step_id: "x",
      answers: { gone: { value: 1 } },
    });
    expect(out.notice).toBe("restarted");
  });
  it("refuses a draft above 16 KB", () => {
    const state = {
      ...start(fixtureTour),
      answers: { notes: { value: "x".repeat(20000) } },
    };
    expect(toDraft(fixtureTour, state)).toBeNull();
  });
});
