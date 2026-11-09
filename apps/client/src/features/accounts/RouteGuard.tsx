import { useEffect, useState, type ReactNode } from "react";
import { Button } from "../design/Button";
import { session, safeReturnTo } from "./session";
export function RouteGuard({ children }: { children: ReactNode }) {
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  useEffect(() => {
    let active = true;
    void session
      .load()
      .then((value) => {
        if (!active) return;
        if (value) setState("ready");
        else
          location.replace(
            `/sign-in?returnTo=${encodeURIComponent(safeReturnTo(location.pathname))}`,
          );
      })
      .catch(() => {
        if (active) setState("error");
      });
    return () => {
      active = false;
    };
  }, []);
  if (state === "ready") return children;
  return (
    <main className="auth-page">
      <p role={state === "error" ? "alert" : "status"}>
        {state === "error"
          ? "Could not open your account. Connect and retry."
          : "Loading your account…"}
      </p>
      {state === "error" && (
        <Button onClick={() => location.reload()}>Retry</Button>
      )}
    </main>
  );
}
