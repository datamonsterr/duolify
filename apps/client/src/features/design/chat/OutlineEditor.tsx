import type { MessageId } from "../localization/messages";
import { useUiPreferences } from "../localization/Preferences";
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
type Notice = { id: MessageId; values?: Record<string, string | number> };
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
  const { t } = useUiPreferences();
  const [internal, setInternal] = useState(initialValue);
  const units = internal;
  const [history, setHistory] = useState<Snapshot[]>([]);
  const [notice, setNotice] = useState<Notice | null>(null);
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
    message: Notice,
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
    change(next, control, id, {
      id: "outline.moved",
      values: { name: item.title, position: destination + 1 },
    });
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
    change(next, "rename", id, { id: "outline.edited" });
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
    change(next, "remove", id, { id: "outline.removed" });
    pendingFocus.current = { undo: true };
  }
  function add(parent?: string) {
    const id = `${namespace}-new-${++nextId.current}`;
    const next = parent
      ? units.map((unit) =>
          unit.id === parent
            ? {
                ...unit,
                lessons: [
                  ...unit.lessons,
                  { id, title: t("outline.new_lesson") },
                ],
              }
            : unit,
        )
      : [...units, { id, title: t("outline.new_unit"), lessons: [] }];
    change(
      next,
      "add",
      id,
      { id: parent ? "outline.lesson_added" : "outline.unit_added" },
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
    const title =
      item.title ||
      (parent ? t("outline.untitled_lesson") : t("outline.untitled_unit"));
    return (
      <div className="outline-controls">
        <Button
          variant="quiet"
          draggable
          aria-label={t("outline.reorder", { name: title })}
          title={t("outline.drag_help")}
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
          aria-label={t("outline.move_up", { name: title })}
          aria-disabled={index === 0}
          onClick={() => move(item.id, parent, index - 1, "move_up")}
        >
          ↑{" "}
          <span className="guided-sr-only">
            {index === 0 ? t("outline.first") : t("outline.up")}
          </span>
        </Button>
        <Button
          variant="quiet"
          aria-label={t("outline.move_down", { name: title })}
          aria-disabled={index === length - 1}
          onClick={() => move(item.id, parent, index + 1, "move_down")}
        >
          ↓{" "}
          <span className="guided-sr-only">
            {index === length - 1 ? t("outline.last") : t("outline.down")}
          </span>
        </Button>
        <Button
          variant="quiet"
          aria-label={t("outline.remove_named", { name: title })}
          onClick={() => remove(item.id, parent)}
        >
          {t("outline.remove")}
        </Button>
      </div>
    );
  }
  return (
    <div ref={root} className="outline-editor">
      <header className="guided-panel-header">
        <h2>{t("outline.title")}</h2>
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
            setNotice({ id: "outline.undone" });
            track("undo");
            focusTitle(previous.focusId);
          }}
        >
          {t("common.undo")}
        </button>
      </header>
      <p>{t("outline.intro")}</p>
      {shown.length === 0 && <p>{t("outline.empty")}</p>}
      <ol className="outline-units">
        {shown.map((unit, index) => (
          <li key={unit.id} data-outline-id={unit.id} className="outline-unit">
            <div className="outline-row">
              <span className="outline-position">
                {t("outline.unit", { index: index + 1 })}
              </span>
              <Field
                label={t("outline.title_for", {
                  name: unit.title || t("outline.untitled_unit"),
                })}
                value={unit.title}
                error={!unit.title.trim() ? t("outline.unit_error") : undefined}
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
                    {t("outline.lesson", {
                      unit: index + 1,
                      lesson: lessonIndex + 1,
                    })}
                  </span>
                  <div>
                    <Field
                      label={t("outline.title_for", {
                        name: lesson.title || t("outline.untitled_lesson"),
                      })}
                      value={lesson.title}
                      error={
                        !lesson.title.trim()
                          ? t("outline.lesson_error")
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
              {t("outline.add_lesson", {
                name: unit.title || t("outline.untitled_unit"),
              })}
            </Button>
          </li>
        ))}
      </ol>
      <Button variant="secondary" onClick={() => add()}>
        {t("outline.add_unit")}
      </Button>
      <p role="status" aria-live="polite" aria-atomic="true">
        {notice ? t(notice.id, notice.values) : ""}
      </p>
    </div>
  );
}
