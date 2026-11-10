import { useUiPreferences } from "./localization/Preferences";
import type { ButtonHTMLAttributes, Ref } from "react";

type Props = ButtonHTMLAttributes<HTMLButtonElement> & {
  ref?: Ref<HTMLButtonElement>;
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
  const { t } = useUiPreferences();
  return (
    <button
      {...props}
      type={props.type ?? "button"}
      className={`duo-button ${variant} ${className}`}
      disabled={disabled || pending}
      aria-busy={pending || undefined}
    >
      {pending ? t("common.preparing") : children}
    </button>
  );
}
