import {
  useEffect,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { Button } from "../design/Button";
import { trackSessionRecovery } from "../../lib/analytics";
import { SignIn } from "./SignIn";
import { session, safeReturnTo } from "./session";
export function RouteGuard({
  children,
  path,
}: {
  children: ReactNode;
  path?: string;
}) {
  useSyncExternalStore(session.subscribe, session.snapshot);
  const [owner, setOwner] = useState<string | null>(null);
  const [error, setError] = useState(false);
  const [recovering, setRecovering] = useState(false);
  async function check() {
    setError(false);
    try {
      const value = await session.load();
      if (value) setOwner(value.id);
      else if (!session.expired())
        location.replace(
          `/sign-in?returnTo=${encodeURIComponent(safeReturnTo(location.pathname))}`,
        );
    } catch {
      setError(true);
    }
  }
  useEffect(() => {
    let active = true;
    const recheck = () => {
      void session
        .load()
        .then((value) => {
          if (!active) return;
          if (value) {
            setOwner(value.id);
            setError(false);
          } else if (!session.expired())
            location.replace(
              `/sign-in?returnTo=${encodeURIComponent(safeReturnTo(location.pathname))}`,
            );
        })
        .catch(() => {
          if (active) setError(true);
        });
    };
    recheck();
    window.addEventListener("focus", recheck);
    window.addEventListener("online", recheck);
    return () => {
      active = false;
      window.removeEventListener("focus", recheck);
      window.removeEventListener("online", recheck);
    };
  }, [path]);
  const current = session.current();
  const blocked = session.expired() || !current;
  return (
    <>
      {owner && (
        <div key={current?.id ?? owner} hidden={blocked} inert={blocked}>
          {error && (
            <div role="alert" className="auth-warning">
              Could not verify your session. Connect and retry.
              <Button onClick={() => void check()}>Retry</Button>
            </div>
          )}
          {children}
        </div>
      )}
      {session.expired() ? (
        recovering ? (
          <SignIn
            recovery
            onSignedIn={() => {
              setOwner(session.current()?.id ?? null);
              setRecovering(false);
              setError(false);
            }}
          />
        ) : (
          <main className="auth-page">
            <h1>Session expired</h1>
            <p>Your draft is kept on this device. Sign in again to continue.</p>
            <Button
              onClick={() => {
                trackSessionRecovery();
                setRecovering(true);
              }}
            >
              Sign in again
            </Button>
          </main>
        )
      ) : (
        !owner && (
          <main className="auth-page">
            <p role={error ? "alert" : "status"}>
              {error
                ? "Could not open your account. Connect and retry."
                : "Loading your account…"}
            </p>
            {error && <Button onClick={() => void check()}>Retry</Button>}
          </main>
        )
      )}
    </>
  );
}
