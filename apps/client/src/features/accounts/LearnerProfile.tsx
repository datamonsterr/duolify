import { useEffect, useState } from "react";
import { LearnerContextEditor } from "./LearnerContextEditor";
import { profileApi, type LearnerContext } from "./learner-context";
import { Feedback } from "../design/chat/Feedback";
import { Button } from "../design/Button";

export function LearnerProfile({ setup = false }: { setup?: boolean }) {
  const [profile, setProfile] = useState<LearnerContext | null>(null);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    void profileApi
      .read()
      .then((value) => {
        if (active) setProfile(value);
      })
      .catch(() => {
        if (active) setError("Could not read your profile. Connect and retry.");
      });
    return () => {
      active = false;
    };
  }, [attempt]);
  if (!profile)
    return (
      <div aria-busy={!error}>
        {error ? (
          <>
            <Feedback tone="danger" title={error} blocking />
            <Button
              onClick={() => {
                setError("");
                setAttempt((value) => value + 1);
              }}
            >
              Retry profile
            </Button>
          </>
        ) : (
          <p role="status">Loading learner context…</p>
        )}
      </div>
    );
  return (
    <LearnerContextEditor
      profile={profile}
      setup={setup}
      onCommit={profileApi.commit}
      onExport={async () => {
        const exported = await profileApi.export();
        const url = URL.createObjectURL(
          new Blob([JSON.stringify(exported, null, 2)], {
            type: "application/json",
          }),
        );
        const link = document.createElement("a");
        link.href = url;
        link.download = "learner-profile.json";
        link.click();
        URL.revokeObjectURL(url);
      }}
    />
  );
}
export function SetupProfile() {
  return <LearnerProfile setup />;
}
