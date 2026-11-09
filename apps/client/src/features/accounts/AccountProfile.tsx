import { useEffect, useState } from "react";
import { Button } from "../design/Button";
import { session, type Learner } from "./session";
export function AccountProfile() {
  const [learner, setLearner] = useState<Learner | null>(session.current());
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  useEffect(() => {
    void session
      .load()
      .then(setLearner)
      .catch(() => setError("Could not read your account. Please retry."));
  }, []);
  return (
    <section data-learner-id={learner?.id}>
      <h1>Profile</h1>
      {learner ? (
        <>
          <p>Signed in as {learner.display_name}</p>
          <p>
            {learner.locale} · {learner.time_zone}
          </p>
          <Button
            pending={pending}
            onClick={() => {
              setPending(true);
              void session
                .logout()
                .then(() => location.assign("/sign-in?returning=true"))
                .catch(() => {
                  setError("Could not sign out. Connect and retry.");
                  setPending(false);
                });
            }}
          >
            Sign out
          </Button>
        </>
      ) : (
        <p role="status">Loading your account…</p>
      )}
      {error && <p role="alert">{error}</p>}
    </section>
  );
}
