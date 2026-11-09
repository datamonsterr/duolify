import type { ReactNode } from "react";

type Role = "info" | "credit" | "streak" | "success" | "warning" | "danger";
export function Badge({ role, children }: { role: Role; children: ReactNode }) {
  return <span className={`badge badge-${role}`}>{children}</span>;
}
