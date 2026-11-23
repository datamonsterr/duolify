import { useId, useRef, useState } from "react";
import type { FormEvent, KeyboardEvent, ReactNode } from "react";
import { Button } from "../../design/Button";
import { useUiPreferences } from "../../design/localization/Preferences";
import type { MessageId } from "../../design/localization/messages";
import type { Step, Value } from "../types";

export type WidgetProps = {
  step: Step;
  /** Id of the guide bubble that asks the question; labels every control. */
  promptId: string;
  initial: Value | undefined;
  error: string | null;
  disabled?: boolean;
  editing?: boolean;
  /** Confirm card: rows already localised by the caller. */
  summary?: { label: string; answer: string }[];
  onSubmit: (value: Value, file?: File) => void;
  onSkip?: () => void;
  onCancel?: () => void;
};

const t0 = (k: string) => k as MessageId;

function Chips({
  step,
  name,
  promptId,
  multiple,
  value,
  onChange,
}: {
  step: Step;
  name: string;
  promptId: string;
  multiple: boolean;
  value: string[];
  onChange: (next: string[]) => void;
}) {
  const { t } = useUiPreferences();
  return (
    <div
      className="gc-chips"
      role={multiple ? "group" : "radiogroup"}
      aria-labelledby={promptId}
    >
      {step.options?.map((o) => (
        <label className="gc-chip" key={o.id}>
          <input
            type={multiple ? "checkbox" : "radio"}
            name={name}
            value={o.id}
            checked={value.includes(o.id)}
            onChange={(e) =>
              onChange(
                multiple
                  ? e.target.checked
                    ? [...value, o.id]
                    : value.filter((v) => v !== o.id)
                  : [o.id],
              )
            }
          />
          <span>{t(t0(o.labelKey))}</span>
        </label>
      ))}
    </div>
  );
}

export function StepWidget(props: WidgetProps) {
  const { step, promptId, initial, error, disabled, editing, summary } = props;
  const { t } = useUiPreferences();
  const name = useId();
  const errorId = useId();
  const file = useRef<File | undefined>(undefined);
  const [value, setValue] = useState<Value>(
    initial ??
      step.initial ??
      (step.widget === "multi"
        ? []
        : step.widget === "number"
          ? (step.validate?.type === "number" && step.validate.min) || 1
          : step.widget === "toggle"
            ? null
            : ""),
  );
  const text0 = typeof value === "string" ? value : "";

  function submit(event: FormEvent) {
    event.preventDefault();
    if (props.disabled) return;
    if (step.widget === "confirm") return props.onSubmit(true);
    if (step.widget === "number") return props.onSubmit(Number(value));
    props.onSubmit(
      typeof value === "string" &&
        !["single", "chips+text", "time", "date", "password", "code"].includes(
          step.widget,
        )
        ? value.trim()
        : value,
      file.current,
    );
  }
  function keys(event: KeyboardEvent) {
    if (event.key === "Escape" && editing && props.onCancel) {
      event.preventDefault();
      props.onCancel();
    }
  }
  const describedBy = error ? errorId : undefined;
  const invalid = error ? true : undefined;
  let control: ReactNode;
  switch (step.widget) {
    case "single":
    case "chips+text":
    case "multi":
      control = (
        <>
          <Chips
            step={step}
            name={name}
            promptId={promptId}
            multiple={step.widget === "multi"}
            value={
              step.widget === "multi"
                ? (value as string[])
                : typeof value === "string" && value
                  ? [value]
                  : []
            }
            onChange={(next) =>
              setValue(step.widget === "multi" ? next : next[0])
            }
          />
        </>
      );
      break;
    case "toggle":
      control = (
        <div className="gc-chips" role="radiogroup" aria-labelledby={promptId}>
          {[true, false].map((on) => (
            <label className="gc-chip" key={String(on)}>
              <input
                type="radio"
                name={name}
                checked={value === on}
                onChange={() => setValue(on)}
              />
              <span>{t(on ? "guided.yes" : "guided.no")}</span>
            </label>
          ))}
        </div>
      );
      break;
    case "rating": {
      const max = step.validate?.type === "rating" ? step.validate.max : 5;
      control = (
        <div className="gc-chips" role="radiogroup" aria-labelledby={promptId}>
          {Array.from({ length: max }, (_, i) => i + 1).map((n) => (
            <label className="gc-chip" key={n}>
              <input
                type="radio"
                name={name}
                checked={value === n}
                aria-label={t("guided.stars", { count: n })}
                onChange={() => setValue(n)}
              />
              <span aria-hidden="true">{`${n} ★`}</span>
            </label>
          ))}
        </div>
      );
      break;
    }
    case "time":
    case "date":
      control = (
        <input
          type={step.widget}
          name={name}
          aria-labelledby={promptId}
          aria-describedby={describedBy}
          aria-invalid={invalid}
          value={text0}
          onChange={(e) => setValue(e.target.value)}
        />
      );
      break;
    case "number": {
      const rule = step.validate?.type === "number" ? step.validate : null;
      const bump = (d: number) =>
        setValue(
          Math.min(
            rule?.max ?? 1e6,
            Math.max(rule?.min ?? 0, Number(value || 0) + d),
          ),
        );
      control = (
        <div className="gc-stepper">
          <Button
            variant="secondary"
            aria-label={t("guided.less")}
            onClick={() => bump(-1)}
          >
            −
          </Button>
          <input
            type="number"
            name={name}
            inputMode="numeric"
            min={rule?.min}
            max={rule?.max}
            aria-labelledby={promptId}
            aria-describedby={describedBy}
            aria-invalid={invalid}
            value={value === "" ? "" : String(value)}
            onChange={(e) =>
              setValue(e.target.value === "" ? "" : Number(e.target.value))
            }
          />
          <Button
            variant="secondary"
            aria-label={t("guided.more")}
            onClick={() => bump(1)}
          >
            +
          </Button>
        </div>
      );
      break;
    }
    case "text":
      control = (
        <input
          type="text"
          name={name}
          dir="auto"
          aria-labelledby={promptId}
          aria-describedby={describedBy}
          aria-invalid={invalid}
          value={text0}
          onChange={(e) => setValue(e.target.value)}
        />
      );
      break;
    case "longtext":
      control = (
        <textarea
          name={name}
          rows={3}
          dir="auto"
          aria-labelledby={promptId}
          aria-describedby={describedBy}
          aria-invalid={invalid}
          value={text0}
          onChange={(e) => setValue(e.target.value)}
        />
      );
      break;
    case "file": {
      const rule = step.validate?.type === "file" ? step.validate : null;
      control = (
        <div className="gc-file">
          <input
            type="file"
            name={name}
            accept={rule?.accept.join(",")}
            aria-labelledby={promptId}
            aria-describedby={describedBy}
            aria-invalid={invalid}
            onChange={(e) => {
              const picked = e.target.files?.[0];
              file.current = picked;
              setValue(
                picked && picked.size <= (rule?.maxBytes ?? 0)
                  ? picked.name
                  : "",
              );
            }}
          />
          <span className="field-hint">{text0 || t("guided.file_none")}</span>
        </div>
      );
      break;
    }
    case "email":
    case "code":
    case "password":
      control = (
        <input
          type={
            step.widget === "email"
              ? "email"
              : step.widget === "password"
                ? "password"
                : "text"
          }
          name={step.input?.name}
          autoComplete={step.input?.autocomplete}
          inputMode={step.input?.inputMode as "email" | "numeric" | undefined}
          autoCapitalize="none"
          spellCheck={false}
          aria-labelledby={promptId}
          aria-describedby={describedBy}
          aria-invalid={invalid}
          value={text0}
          onChange={(e) => setValue(e.target.value)}
        />
      );
      break;
    case "confirm":
      control = (
        <dl className="gc-summary">
          {summary?.map((row) => (
            <div key={row.label}>
              <dt>{row.label}</dt>
              <dd dir="auto">{row.answer}</dd>
            </div>
          ))}
        </dl>
      );
      break;
  }
  const destructive = step.widget === "confirm" && step.destructive;
  return (
    <form
      className="gc-widget"
      data-widget={step.widget}
      onSubmit={submit}
      onKeyDown={keys}
      noValidate
    >
      {control}
      {error && (
        <p id={errorId} role="alert" className="field-error">
          {error}
        </p>
      )}
      <div className="gc-actions">
        {destructive && props.onCancel && (
          <Button variant="secondary" data-autofocus onClick={props.onCancel}>
            {t("guided.cancel")}
          </Button>
        )}
        <Button
          type="submit"
          disabled={disabled}
          className={destructive ? "gc-danger" : ""}
        >
          {t(step.widget === "confirm" ? "guided.confirm" : "guided.send")}
        </Button>
        {props.onSkip && step.skippable && !editing && (
          <Button variant="quiet" onClick={props.onSkip}>
            {t("guided.skip")}
          </Button>
        )}
        {editing && !destructive && props.onCancel && (
          <Button variant="quiet" onClick={props.onCancel}>
            {t("guided.cancel")}
          </Button>
        )}
      </div>
    </form>
  );
}
