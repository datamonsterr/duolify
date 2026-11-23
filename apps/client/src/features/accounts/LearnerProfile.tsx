import { useEffect, useState } from "react";
import { LearnerContextEditor } from "./LearnerContextEditor";
import { SetupChat } from "./SetupChat";
import { GuidedSetup } from "../guided-chat/GuidedSetup";
import { guidedEnabled } from "../guided-chat/flags";
import { profileApi, type LearnerContext } from "./learner-context";
import { Feedback } from "../design/chat/Feedback";
import { Button } from "../design/Button";

export function LearnerProfile({
  setup = false,
  entry = "onboarding",
}: {
  setup?: boolean;
  entry?: "onboarding" | "profile";
}) {
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
  const exportProfile = async () => {
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
  };

  if (setup && guidedEnabled("onboarding", profile.learner_id)) {
    return (
      <GuidedSetup
        profile={profile}
        onCommit={profileApi.commit}
        entryPoint={entry}
      />
    );
  }
  if (setup) {
    return (
      <SetupChat
        profile={profile}
        onCommit={profileApi.commit}
        onExport={exportProfile}
      />
    );
  }

  return (
    <LearnerContextEditor
      profile={profile}
      setup={false}
      onCommit={profileApi.commit}
      onExport={exportProfile}
    />
  );
}
export function SetupProfile({
  entry = "onboarding",
}: {
  entry?: "onboarding" | "profile";
}) {
  return <LearnerProfile setup entry={entry} />;
}
