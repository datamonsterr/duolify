import {
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type DragEvent,
} from "react";
import { trackGuidedUi, type GuidedUiEvent } from "../../../lib/analytics";
import { Button } from "../Button";
import { Field } from "./Feedback";
import "./guided-chat.css";

export type OutlineLesson = { id: string; title: string; goal?: string };
export type OutlineUnit = {
  id: string;
  title: string;
  lessons: OutlineLesson[];
};
export type Outline = OutlineUnit[];
type Control = Extract<
  GuidedUiEvent,
  { event: "ui_control_activated" }
>["controlId"];
type Snapshot = { value: Outline; focusId: string };

// Features own proposal approval; passing a new proposal snapshot records one
// undoable edit. onChange returns manual edits and Undo to the owning feature.
export function OutlineEditor({
  initialValue,
  onChange,
  proposal,
}: {
  initialValue: Outline;
  onChange?: (value: Outline) => void;
  proposal?: Outline | null;
}) {
  const [internal, setInternal] = useState(initialValue);
  const units = internal;
  const [history, setHistory] = useState<Snapshot[]>([]);
  const [notice, setNotice] = useState("");
  const root = useRef<HTMLDivElement>(null);
  const undo = useRef<HTMLButtonElement>(null);
  const dragging = useRef<{ id: string; parent?: string } | null>(null);
  const namespace = useId();
  const nextId = useRef(0);
  const pendingFocus = useRef<{ id?: string; undo?: boolean } | null>(null);
  useLayoutEffect(() => {
    const target = pendingFocus.current;
    if (!target) return;
    pendingFocus.current = null;
    if (target.undo) undo.current?.focus();
    else
      [
        ...(root.current?.querySelectorAll<HTMLElement>("[data-outline-id]") ??
          []),
      ]
        .find((node) => node.dataset.outlineId === target.id)
        ?.querySelector<HTMLInputElement>("input")
        ?.focus();
  });
  const [appliedProposal, setAppliedProposal] = useState<Outline | null>(null);
  // React's documented previous-prop adjustment avoids an effect applying twice.
  if (proposal && proposal !== appliedProposal) {
    setAppliedProposal(proposal);
    setHistory([...history, { value: units, focusId: units[0]?.id ?? "" }]);
    setInternal(proposal);
  }
  const shown = proposal && proposal !== appliedProposal ? proposal : units;
  function track(controlId: Control) {
    trackGuidedUi({
      event: "ui_control_activated",
      surface: "outline",
      controlId,
    });
  }
  function publish(next: Outline) {
    setInternal(next);
    onChange?.(next);
  }
  function focusTitle(id: string) {
    pendingFocus.current = { id };
  }
  function change(
    next: Outline,
    control: Control,
    focusId: string,
    message: string,
    focus = false,
  ) {
    setHistory([...history, { value: units, focusId }]);
    publish(next);
    setNotice(message);
    track(control);
    if (focus) focusTitle(focusId);
  }
  function move(
    id: string,
    parent: string | undefined,
    destination: number,
    control: Control,
  ) {
    const list = parent
      ? units.find((unit) => unit.id === parent)?.lessons
      : units;
    if (!list || destination < 0 || destination >= list.length) return;
    const from = list.findIndex((item) => item.id === id);
    if (from < 0 || from === destination) return;
    const nextList = [...list];
    const [item] = nextList.splice(from, 1);
    nextList.splice(destination, 0, item);
    const next = parent
      ? units.map((unit) =>
          unit.id === parent
            ? { ...unit, lessons: nextList as OutlineLesson[] }
            : unit,
        )
      : (nextList as Outline);
    change(
      next,
      control,
      id,
      `${item.title} moved to position ${destination + 1}`,
    );
  }
  function rename(id: string, parent: string | undefined, title: string) {
    const next = parent
      ? units.map((unit) =>
          unit.id === parent
            ? {
                ...unit,
                lessons: unit.lessons.map((lesson) =>
                  lesson.id === id ? { ...lesson, title } : lesson,
                ),
              }
            : unit,
        )
      : units.map((unit) => (unit.id === id ? { ...unit, title } : unit));
    change(next, "rename", id, "Title edited. Unsaved changes.");
  }
  function remove(id: string, parent?: string) {
    const next = parent
      ? units.map((unit) =>
          unit.id === parent
            ? {
                ...unit,
                lessons: unit.lessons.filter((lesson) => lesson.id !== id),
              }
            : unit,
        )
      : units.filter((unit) => unit.id !== id);
    change(next, "remove", id, "Item removed. Undo available.");
    pendingFocus.current = { undo: true };
  }
  function add(parent?: string) {
    const id = `${namespace}-new-${++nextId.current}`;
    const next = parent
      ? units.map((unit) =>
          unit.id === parent
            ? {
                ...unit,
                lessons: [...unit.lessons, { id, title: "New lesson" }],
              }
            : unit,
        )
      : [...units, { id, title: "New unit", lessons: [] }];
    change(
      next,
      "add",
      id,
      parent
        ? "Lesson added. Unsaved changes."
        : "Unit added. Unsaved changes.",
      true,
    );
  }
  function drop(event: DragEvent, id: string, parent?: string) {
    event.preventDefault();
    const source = dragging.current;
    if (!source || source.parent !== parent) return;
    const list = parent
      ? units.find((unit) => unit.id === parent)?.lessons
      : units;
    const index = list?.findIndex((item) => item.id === id) ?? -1;
    move(source.id, parent, index, "drag_reorder");
    dragging.current = null;
  }
  function controls(
    item: OutlineLesson,
    index: number,
    length: number,
    parent?: string,
  ) {
    const title = item.title || (parent ? "Untitled lesson" : "Untitled unit");
    return (
      <div className="outline-controls">
        <Button
          variant="quiet"
          draggable
          aria-label={`Reorder ${title}`}
          title="Drag, use arrow keys or move buttons"
          onDragStart={(event) => {
            dragging.current = { id: item.id, parent };
            event.dataTransfer.setData("text/plain", item.id);
            event.dataTransfer.effectAllowed = "move";
          }}
          onDragEnd={() => {
            dragging.current = null;
          }}
          onDragOver={(event) => {
            if (dragging.current?.parent === parent) event.preventDefault();
          }}
          onDrop={(event) => drop(event, item.id, parent)}
          onKeyDown={(event) => {
            if (event.key === "ArrowUp" || event.key === "ArrowDown") {
              event.preventDefault();
              move(
                item.id,
                parent,
                index + (event.key === "ArrowUp" ? -1 : 1),
                event.key === "ArrowUp" ? "move_up" : "move_down",
              );
            }
          }}
        >
          ⠿
        </Button>
        <Button
          variant="quiet"
          aria-label={`Move ${title} up`}
          aria-disabled={index === 0}
          onClick={() => move(item.id, parent, index - 1, "move_up")}
        >
          ↑{" "}
          <span className="guided-sr-only">
            {index === 0 ? "Unavailable, first position" : "Move up"}
          </span>
        </Button>
        <Button
          variant="quiet"
          aria-label={`Move ${title} down`}
          aria-disabled={index === length - 1}
          onClick={() => move(item.id, parent, index + 1, "move_down")}
        >
          ↓{" "}
          <span className="guided-sr-only">
            {index === length - 1 ? "Unavailable, last position" : "Move down"}
          </span>
        </Button>
        <Button
          variant="quiet"
          aria-label={`Remove ${title}`}
          onClick={() => remove(item.id, parent)}
        >
          Remove
        </Button>
      </div>
    );
  }
  return (
    <div ref={root} className="outline-editor">
      <header className="guided-panel-header">
        <h2>Shape your outline</h2>
        <button
          type="button"
          ref={undo}
          className="duo-button secondary"
          disabled={history.length === 0}
          onClick={() => {
            const previous = history[history.length - 1];
            if (!previous) return;
            publish(previous.value);
            setHistory(history.slice(0, -1));
            setNotice("Change undone. Unsaved changes.");
            track("undo");
            focusTitle(previous.focusId);
          }}
        >
          Undo
        </button>
      </header>
      <p>
        Sample outline. Drag units or lessons, use arrow keys on a handle, or
        use move buttons. Lesson content is prepared later.
      </p>
      {shown.length === 0 && <p>No units yet. Add a unit to start.</p>}
      <ol className="outline-units">
        {shown.map((unit, index) => (
          <li key={unit.id} data-outline-id={unit.id} className="outline-unit">
            <div className="outline-row">
              <span className="outline-position">Unit {index + 1}</span>
              <Field
                label={`Title for ${unit.title || "Untitled unit"}`}
                value={unit.title}
                error={!unit.title.trim() ? "Enter a unit title." : undefined}
                onChange={(event) =>
                  rename(unit.id, undefined, event.target.value)
                }
              />
              {controls(unit, index, shown.length)}
            </div>
            <ol className="outline-lessons">
              {unit.lessons.map((lesson, lessonIndex) => (
                <li
                  key={lesson.id}
                  data-outline-id={lesson.id}
                  className="outline-row"
                >
                  <span className="outline-position">
                    Lesson {index + 1}.{lessonIndex + 1}
                  </span>
                  <div>
                    <Field
                      label={`Title for ${lesson.title || "Untitled lesson"}`}
                      value={lesson.title}
                      error={
                        !lesson.title.trim()
                          ? "Enter a lesson title."
                          : undefined
                      }
                      onChange={(event) =>
                        rename(lesson.id, unit.id, event.target.value)
                      }
                    />
                    {lesson.goal && <p>{lesson.goal}</p>}
                  </div>
                  {controls(lesson, lessonIndex, unit.lessons.length, unit.id)}
                </li>
              ))}
            </ol>
            <Button variant="quiet" onClick={() => add(unit.id)}>
              Add a lesson to {unit.title || "Untitled unit"}
            </Button>
          </li>
        ))}
      </ol>
      <Button variant="secondary" onClick={() => add()}>
        Add a unit
      </Button>
      <p role="status" aria-live="polite" aria-atomic="true">
        {notice}
      </p>
    </div>
  );
}
