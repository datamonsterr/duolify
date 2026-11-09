import { useEffect, useRef, useState } from "react";
import { Button } from "../design/Button";
import { session } from "./session";
import "./accounts.css";
export type DeviceSession = {
  id: string;
  created_at: string;
  expires_at: string;
  current: boolean;
};
export function Sessions() {
  const [devices, setDevices] = useState<DeviceSession[] | null>(null);
  const [error, setError] = useState("");
  const [pending, setPending] = useState(true);
  const heading = useRef<HTMLHeadingElement>(null);
  async function load() {
    setPending(true);
    setError("");
    try {
      setDevices(await session.request<DeviceSession[]>("/v1/auth/sessions"));
    } catch {
      setError("Could not read your sessions. Connect and retry.");
    } finally {
      setPending(false);
    }
  }
  useEffect(() => {
    let active = true;
    void session
      .request<DeviceSession[]>("/v1/auth/sessions")
      .then((value) => {
        if (active) setDevices(value);
      })
      .catch(() => {
        if (active)
          setError("Could not read your sessions. Connect and retry.");
      })
      .finally(() => {
        if (active) setPending(false);
      });
    return () => {
      active = false;
    };
  }, []);
  async function revoke(device: DeviceSession) {
    setPending(true);
    setError("");
    try {
      if (device.current) {
        await session.logout();
        location.assign("/sign-in?returning=true");
        return;
      }
      await session.request(`/v1/auth/sessions/${device.id}`, "DELETE");
      await load();
      heading.current?.focus();
    } catch {
      setError("Could not sign out this device. Connect and retry.");
    } finally {
      setPending(false);
    }
  }
  return (
    <section className="sessions-panel" aria-busy={pending}>
      <a className="shell-link" href="/settings">
        Back to Settings
      </a>
      <h1 ref={heading} tabIndex={-1}>
        Sessions
      </h1>
      <p>
        Manage devices signed in to your account. Signing out ends access
        immediately.
      </p>
      <p>
        Unsynced work on a signed-out device may be lost. Connect and save your
        work first.
      </p>
      {pending && <p role="status">Updating sessions…</p>}
      {error && (
        <div className="auth-error" role="alert">
          <p>{error}</p>
          <Button disabled={pending} onClick={() => void load()}>
            Retry
          </Button>
        </div>
      )}
      {devices?.length === 0 && <p role="status">No active sessions.</p>}
      <ul className="sessions-list">
        {devices?.map((device) => (
          <li key={device.id}>
            <div>
              <strong>
                {device.current ? "This device" : "Another device"}
              </strong>
              <p>
                Signed in{" "}
                <time dateTime={device.created_at}>
                  {new Intl.DateTimeFormat(undefined, {
                    dateStyle: "medium",
                    timeStyle: "short",
                  }).format(new Date(device.created_at))}
                </time>
              </p>
              <p>
                Session ends{" "}
                <time dateTime={device.expires_at}>
                  {new Intl.DateTimeFormat(undefined, {
                    dateStyle: "medium",
                  }).format(new Date(device.expires_at))}
                </time>
              </p>
            </div>
            <Button
              variant="secondary"
              disabled={pending}
              onClick={() => void revoke(device)}
            >
              {device.current ? "Sign out this device" : "Sign out device"}
            </Button>
          </li>
        ))}
      </ul>
      <Button
        disabled={pending || !devices?.length}
        onClick={() => {
          setPending(true);
          void session
            .logout(true)
            .then(() => location.assign("/sign-in?returning=true"))
            .catch(() => {
              setError("Could not sign out all devices. Connect and retry.");
              setPending(false);
            });
        }}
      >
        Sign out all devices
      </Button>
    </section>
  );
}
