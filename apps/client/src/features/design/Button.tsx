import { useUiPreferences } from "./localization/Preferences";
import { triggerFeedbackCue } from "./feedback/feedbackBridge";
import type { ButtonHTMLAttributes, Ref } from "react";

type Props = ButtonHTMLAttributes<HTMLButtonElement> & {
  ref?: Ref<HTMLButtonElement>;
  variant?: "primary" | "secondary" | "quiet";
  pending?: boolean;
  /** Hick's law: 1 primary and at most 2 secondary actions per view. Defaults from `variant`. */
  level?: "primary" | "secondary" | "none";
};

export function Button({
  children,
  variant = "primary",
  pending = false,
  level,
  disabled,
  className = "",
  onClick,
  ...props
}: Props) {
  const { t } = useUiPreferences();
  return (
    <button
      {...props}
      type={props.type ?? "button"}
      className={`duo-button ${variant} ${className}`}
      disabled={disabled || pending}
      data-action-level={
        (level ?? (variant === "quiet" ? "none" : variant)) === "none"
          ? undefined
          : (level ?? variant)
      }
      aria-busy={pending || undefined}
      onClick={(event) => {
        if (!disabled && !pending) {
          triggerFeedbackCue("tap");
        }
        onClick?.(event);
      }}
    >
      {pending ? t("common.preparing") : children}
    </button>
  );
}
