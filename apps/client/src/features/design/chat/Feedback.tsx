import { useUiPreferences } from "../localization/Preferences";
import { useId, type InputHTMLAttributes, type ReactNode } from "react";
import { Button } from "../Button";
import { Icon, type IconName } from "../Icon";
import "./guided-chat.css";

export function Field({
  label,
  error,
  hint,
  ...props
}: InputHTMLAttributes<HTMLInputElement> & {
  label: string;
  error?: string;
  hint?: string;
}) {
  const id = useId();
  return (
    <div className="guided-field">
      <label htmlFor={id}>{label}</label>
      <input
        {...props}
        id={id}
        dir="auto"
        aria-invalid={error ? true : undefined}
        aria-describedby={error || hint ? `${id}-help` : undefined}
      />
      {(error || hint) && (
        <span
          id={`${id}-help`}
          className={error ? "field-error" : "field-hint"}
        >
          {error || hint}
        </span>
      )}
    </div>
  );
}

// Colour is never the only signal: every tone carries its own icon.
const toneIcon: Record<"info" | "warning" | "danger" | "success", IconName> = {
  info: "info",
  warning: "warning",
  danger: "error",
  success: "check",
};

export function Feedback({
  tone = "info",
  title,
  children,
  action,
  onAction,
  blocking = false,
}: {
  tone?: "info" | "warning" | "danger" | "success";
  title: string;
  children?: ReactNode;
  action?: string;
  onAction?: () => void;
  blocking?: boolean;
}) {
  return (
    <div
      className={`guided-feedback ${tone}`}
      role={blocking ? "alert" : "status"}
    >
      <div className="guided-step-label">
        <Icon name={toneIcon[tone]} />
        <strong>{title}</strong>
      </div>
      {children && <div>{children}</div>}
      {action && (
        <Button variant="quiet" onClick={onAction}>
          {action}
        </Button>
      )}
    </div>
  );
}

// Persistent feedback is also the toast alternative; it never auto-dismisses work.
export function UndoNotice({
  children,
  onUndo,
}: {
  children: ReactNode;
  onUndo: () => void;
}) {
  const { t } = useUiPreferences();
  return (
    <div className="guided-toast" role="status">
      <span>{children}</span>
      <Button variant="quiet" onClick={onUndo}>
        {t("common.undo")}
      </Button>
    </div>
  );
}
