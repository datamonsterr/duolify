import { ActionFooter } from "../design/sheet/ActionFooter";
import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { LearnerProfile, SetupProfile } from "./LearnerProfile";
import { Button } from "../design/Button";
import { session, type Learner } from "./session";
import { api } from "../../lib/api";
import { profileApi } from "./learner-context";
import { CourseLevelOverride } from "../courses/CourseLevelOverride";
import { GuidedCourseLevel } from "../guided-chat/GuidedCourseLevel";
import { guidedEnabled } from "../guided-chat/flags";

const LevelPicker = (
  props: React.ComponentProps<typeof CourseLevelOverride>,
) =>
  guidedEnabled("course_level") ? (
    <GuidedCourseLevel {...props} />
  ) : (
    <CourseLevelOverride {...props} />
  );

export function AccountProfile() {
  const [learner, setLearner] = useState<Learner | null>(session.current());
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const [chatOpen, setChatOpen] = useState(false);
  const chatRef = useRef<HTMLElement>(null);
  // The chat scrolls itself to its newest line; keep its header in view when it opens.
  useEffect(() => {
    if (chatOpen) chatRef.current?.scrollIntoView?.({ block: "start" });
  }, [chatOpen]);

  const { data: coursesData, refetch: refetchCourses } = useQuery({
    queryKey: ["courses"],
    queryFn: () => api.courses(),
    retry: false,
  });

  const { data: profileContext } = useQuery({
    queryKey: ["learner-profile"],
    queryFn: profileApi.read,
    retry: false,
  });

  useEffect(() => {
    let active = true;
    void session
      .load()
      .then((val) => {
        if (active) setLearner(val);
      })
      .catch(() => {
        if (active) setError("Could not read your account. Please retry.");
      });
    return () => {
      active = false;
    };
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
          <div style={{ display: chatOpen ? "none" : undefined }}>
            <LearnerProfile />
          </div>
          {chatOpen && (
            <section
              ref={chatRef}
              role="dialog"
              aria-label="Update profile by chat"
            >
              <h2>Update by chat</h2>
              <SetupProfile entry="profile" />
              <ActionFooter>
                <Button variant="secondary" onClick={() => setChatOpen(false)}>
                  Close chat
                </Button>
              </ActionFooter>
            </section>
          )}

          <section
            className="per-course-levels card"
            aria-labelledby="per-course-level-heading"
          >
            <h2 id="per-course-level-heading">Per-course level</h2>
            {coursesData && coursesData.length > 0 ? (
              <div className="course-levels-list">
                {coursesData.map((course) => (
                  <LevelPicker
                    key={course.id}
                    courseId={course.id}
                    courseTitle={course.title}
                    levelOverride={course.level_override ?? ""}
                    globalLevel={profileContext?.level || "beginner"}
                    onSaveLevel={async (courseId, level) => {
                      await api.setCourseLevel(courseId, level);
                      void refetchCourses();
                    }}
                  />
                ))}
              </div>
            ) : (
              <p className="muted">No enrolled courses yet.</p>
            )}
          </section>

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
