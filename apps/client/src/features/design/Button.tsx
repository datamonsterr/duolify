import type { ButtonHTMLAttributes } from "react";

type Props = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "quiet";
  pending?: boolean;
};

export function Button({
  children,
  variant = "primary",
  pending = false,
  disabled,
  className = "",
  ...props
}: Props) {
  return (
    <button
      {...props}
      type={props.type ?? "button"}
      className={`duo-button ${variant} ${className}`}
      disabled={disabled || pending}
      aria-busy={pending || undefined}
    >
      {pending ? "Preparing…" : children}
    </button>
  );
}
