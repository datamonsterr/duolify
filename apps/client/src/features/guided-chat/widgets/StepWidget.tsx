import { lazy, Suspense, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { FormEvent, KeyboardEvent, MouseEvent, ReactNode } from "react";
import { Button } from "../../design/Button";
import { ActionFooter } from "../../design/sheet/ActionFooter";
import { useFooterSlot } from "../../design/sheet/FooterSlot";
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

// The selector pulls in the rig and its part library, so it loads only when a script asks.
const CharacterStep = lazy(() => import("./CharacterStepWidget"));

const Svg = ({ d }: { d: string }) => (
  <svg
    viewBox="0 0 24 24"
    width="22"
    height="22"
    aria-hidden="true"
    focusable="false"
    fill="none"
    stroke="currentColor"
    strokeWidth="2.5"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <path d={d} />
  </svg>
);
export const SEND_PATH = "M5 12h14M13 6l6 6-6 6";

/** One round icon button. `quiet` keeps it out of sight until a keyboard user focuses it. */
export function IconSubmit({
  label,
  path = SEND_PATH,
  quiet,
  disabled,
}: {
  label: string;
  path?: string;
  quiet?: boolean;
  disabled?: boolean;
}) {
  return (
    <button
      type="submit"
      className={quiet ? "gc-send gc-send-quiet" : "gc-send"}
      aria-label={label}
      disabled={disabled}
    >
      <Svg d={path} />
    </button>
  );
}

function Chips({
  step,
  name,
  promptId,
  multiple,
  value,
  onChange,
  onTap,
  children,
}: {
  step: Step;
  name: string;
  promptId: string;
  multiple: boolean;
  value: string[];
  onChange: (next: string[]) => void;
  onTap?: (event: MouseEvent, id: string) => void;
  children?: ReactNode;
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
            onClick={(e) => onTap?.(e, o.id)}
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
      {children}
    </div>
  );
}

export function StepWidget(props: WidgetProps) {
  const { step, promptId, initial, error, disabled, editing, summary } = props;
  const { t } = useUiPreferences();
  const name = useId();
  const errorId = useId();
  const formId = useId();
  const slot = useFooterSlot();
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
  const [ticked, setTicked] = useState<string[]>([]);
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
  /** A pointer tap answers at once; keyboard selection waits for Enter on the hidden Send. */
  const tap = (event: MouseEvent, v: Value) => {
    if (event.detail > 0 && !props.disabled) props.onSubmit(v);
  };
  const skipChip =
    props.onSkip && step.skippable && !editing ? (
      <button type="button" className="gc-chip gc-skip" onClick={props.onSkip}>
        {t("guided.skip")}
      </button>
    ) : null;
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
            onTap={step.widget === "multi" ? undefined : tap}
          >
            {skipChip}
          </Chips>
        </>
      );
      break;
    case "character":
      control = (
        <Suspense fallback={<p role="status">{t("guided.loading")}</p>}>
          <CharacterStep
            step={step}
            promptId={promptId}
            disabled={disabled}
            value={typeof value === "string" ? value : ""}
            onChange={setValue}
          />
        </Suspense>
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
                onClick={(e) => tap(e, on)}
                onChange={() => setValue(on)}
              />
              <span>{t(on ? "guided.yes" : "guided.no")}</span>
            </label>
          ))}
          {skipChip}
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
                onClick={(e) => tap(e, n)}
                onChange={() => setValue(n)}
              />
              <span aria-hidden="true">{`${n} ★`}</span>
            </label>
          ))}
          {skipChip}
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
          onKeyDown={(e) => {
            if (
              e.key === "Enter" &&
              !e.shiftKey &&
              !e.nativeEvent.isComposing
            ) {
              e.preventDefault();
              e.currentTarget.form?.requestSubmit();
            }
          }}
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
          {...(step.input?.labelKey
            ? { "aria-label": t(t0(step.input.labelKey)) }
            : { "aria-labelledby": promptId })}
          aria-describedby={describedBy}
          aria-invalid={invalid}
          value={text0}
          onChange={(e) => setValue(e.target.value)}
        />
      );
      break;
    case "confirm":
      control = (
        <>
          {summary && summary.length > 0 && (
            <dl className="gc-summary">
              {summary?.map((row) => (
                <div key={row.label}>
                  <dt>{row.label}</dt>
                  <dd dir="auto">{row.answer}</dd>
                </div>
              ))}
            </dl>
          )}
          {step.checks && (
            <fieldset className="gc-checks" aria-labelledby={promptId}>
              {step.checks.map((id) => (
                <label className="gc-check" key={id}>
                  <input
                    type="checkbox"
                    name={id}
                    checked={ticked.includes(id)}
                    onChange={(e) =>
                      setTicked((now) =>
                        e.target.checked
                          ? [...now, id]
                          : now.filter((x) => x !== id),
                      )
                    }
                  />
                  <span>{t(t0(id))}</span>
                </label>
              ))}
            </fieldset>
          )}
        </>
      );
      break;
  }
  const blocked = Boolean(
    step.checks && step.checks.some((id) => !ticked.includes(id)),
  );
  const destructive = step.widget === "confirm" && step.destructive;
  const labelled = step.widget === "confirm" && !destructive;
  const max = step.validate?.type === "string" ? step.validate.max : undefined;
  const count = [...text0.trim()].length;
  const showCount =
    max !== undefined &&
    (step.widget === "longtext" || step.widget === "text") &&
    count >= max * 0.8;
  const chipWidget = [
    "single",
    "chips+text",
    "toggle",
    "rating",
    "multi",
  ].includes(step.widget);
  const hasValue = Array.isArray(value)
    ? value.length > 0
    : typeof value === "string"
      ? value.trim() !== ""
      : value !== null;
  // Chip steps answer on tap, so their Send stays out of sight until keyboard focus.
  const quiet = chipWidget && step.widget !== "multi";
  const showSend =
    step.widget === "confirm"
      ? !destructive
      : step.widget === "character"
        ? false
        : quiet || hasValue || step.widget === "longtext";
  const inline = [
    "text",
    "longtext",
    "email",
    "code",
    "password",
    "number",
    "time",
    "date",
  ];
  const skip = !chipWidget && props.onSkip && step.skippable && !editing && (
    <Button variant="quiet" onClick={props.onSkip}>
      {t("guided.skip")}
    </Button>
  );
  // Inside a dialog with a footer slot, that footer's Cancel replaces the chat's own.
  const cancel =
    !slot &&
    props.onCancel &&
    (destructive ? (
      <Button variant="secondary" data-autofocus onClick={props.onCancel}>
        {t("guided.cancel")}
      </Button>
    ) : (
      editing && (
        <Button variant="quiet" onClick={props.onCancel}>
          {t("guided.cancel")}
        </Button>
      )
    ));
  const primary = (destructive || (labelled && showSend)) && (
    <Button
      type="submit"
      form={formId}
      disabled={disabled || blocked}
      className={destructive ? "gc-danger" : undefined}
    >
      {t(step.actionKey ? t0(step.actionKey) : "guided.confirm")}
    </Button>
  );
  const icon = !inline.includes(step.widget) && !labelled && showSend && (
    <IconSubmit
      quiet={quiet}
      label={t(step.actionKey ? t0(step.actionKey) : "guided.send")}
      disabled={disabled || blocked}
    />
  );
  return (
    <form
      id={formId}
      className="gc-widget"
      data-widget={step.widget}
      onSubmit={submit}
      onKeyDown={keys}
      noValidate
    >
      <div
        className={inline.includes(step.widget) ? "gc-field-row" : undefined}
      >
        {control}
        {inline.includes(step.widget) && showSend && (
          <IconSubmit
            label={t(step.actionKey ? t0(step.actionKey) : "guided.send")}
            disabled={disabled}
          />
        )}
      </div>
      {showCount && (
        <p
          className={
            count > max! ? "field-error gc-count" : "field-hint gc-count"
          }
          aria-live="polite"
        >
          {count}/{max}
        </p>
      )}
      {error && (
        <p id={errorId} role="alert" className="field-error">
          {error}
        </p>
      )}
      {slot ? (
        <>
          <ActionFooter className="gc-actions" hidden={!icon}>
            {icon}
          </ActionFooter>
          {(skip || primary) &&
            createPortal(
              <>
                {skip}
                {primary}
              </>,
              slot,
            )}
        </>
      ) : (
        <ActionFooter
          className="gc-actions"
          hidden={!(skip || cancel || primary || icon)}
        >
          {skip}
          {cancel}
          {primary}
          {icon}
        </ActionFooter>
      )}
    </form>
  );
}
