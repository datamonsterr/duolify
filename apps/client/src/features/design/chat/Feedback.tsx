import { useId, type InputHTMLAttributes, type ReactNode } from "react";
import { Button } from "../Button";
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
      <strong>{title}</strong>
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
  return (
    <div className="guided-toast" role="status">
      <span>{children}</span>
      <Button variant="quiet" onClick={onUndo}>
        Undo
      </Button>
    </div>
  );
}
