import { useEffect, useState } from "react";
import { LearnerProfile } from "./LearnerProfile";
import { Button } from "../design/Button";
import { session, type Learner } from "./session";
export function AccountProfile() {
  const [learner, setLearner] = useState<Learner | null>(session.current());
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const [chatOpen, setChatOpen] = useState(false);
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
          <a className="shell-link" href="/profile/setup">
            Set up learner context
          </a>
          <Button variant="secondary" onClick={() => setChatOpen(true)}>
            Update by chat
          </Button>
          <LearnerProfile />
          {chatOpen && (
            <section role="dialog" aria-label="Update profile by chat">
              <h2>Update by chat</h2>
              <p>
                Chat profile updates are coming in E1-11. Your form draft is
                still here.
              </p>
              <Button variant="secondary" onClick={() => setChatOpen(false)}>
                Close chat
              </Button>
            </section>
          )}
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
