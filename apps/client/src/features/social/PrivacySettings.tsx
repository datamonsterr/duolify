import { useEffect, useId, useState } from "react";
import { session } from "../accounts/session";
import "./social.css";

export interface SocialSettings {
  discoverable: boolean;
  request_policy: "everyone" | "friends_of_friends" | "nobody";
  show_streak_to_friends: boolean;
  course_invites_from: "friends" | "everyone" | "nobody";
}
const settingsPath = "/v1/social/settings";

/** Privacy & social controls (S23). Each change saves alone; a failed save keeps the old value. */
export function PrivacySettings() {
  const id = useId();
  const [settings, setSettings] = useState<SocialSettings>();
  const [loadError, setLoadError] = useState(false);
  const [revision, setRevision] = useState(0);
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState("");
  const [problem, setProblem] = useState("");

  useEffect(() => {
    let active = true;
    session
      .request<SocialSettings>(settingsPath)
      .then((value) => {
        if (active) {
          setSettings(value);
          setLoadError(false);
        }
      })
      .catch(() => {
        if (active) setLoadError(true);
      });
    return () => {
      active = false;
    };
  }, [revision]);

  async function save(label: string, patch: Partial<SocialSettings>) {
    setSaving(true);
    setNotice("");
    setProblem("");
    try {
      setSettings(
        await session.request<SocialSettings>(settingsPath, "PATCH", patch),
      );
      setNotice("Saved.");
    } catch {
      setProblem(
        `Couldn't save “${label}”. Your previous choice is kept. Try again.`,
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="social-privacy" aria-labelledby={`${id}-title`}>
      <h2 id={`${id}-title`}>Privacy &amp; social</h2>
      {loadError && (
        <div role="alert" className="social-error">
          <p>Couldn't load your privacy settings.</p>
          <button
            type="button"
            className="duo-button quiet"
            onClick={() => setRevision((x) => x + 1)}
          >
            Retry
          </button>
        </div>
      )}
      {!settings && !loadError && (
        <p role="status" aria-busy="true">
          Loading privacy settings…
        </p>
      )}
      {settings && (
        <div className="social-privacy-controls" aria-busy={saving}>
          <label className="social-setting">
            <input
              type="checkbox"
              checked={settings.discoverable}
              disabled={saving}
              aria-describedby={`${id}-find`}
              onChange={(event) =>
                void save("Findable by username", {
                  discoverable: event.target.checked,
                })
              }
            />
            <span>Findable by username</span>
          </label>
          <small id={`${id}-find`}>
            Off: you are left out of prefix search. Someone who knows your exact
            username can still find you while requests are open to everyone.
          </small>
          <label className="social-setting stacked">
            <span>Requests and invites from</span>
            <select
              value={settings.request_policy}
              disabled={saving}
              onChange={(event) =>
                void save("Requests and invites from", {
                  request_policy: event.target
                    .value as SocialSettings["request_policy"],
                })
              }
            >
              <option value="everyone">Everyone</option>
              <option value="friends_of_friends">Friends of friends</option>
              <option value="nobody">Nobody</option>
            </select>
          </label>
          <label className="social-setting stacked">
            <span>Course invites from</span>
            <select
              value={settings.course_invites_from}
              disabled={saving}
              onChange={(event) =>
                void save("Course invites from", {
                  course_invites_from: event.target
                    .value as SocialSettings["course_invites_from"],
                })
              }
            >
              <option value="friends">Friends</option>
              <option value="everyone">Everyone</option>
              <option value="nobody">Nobody</option>
            </select>
          </label>
          <label className="social-setting">
            <input
              type="checkbox"
              checked={settings.show_streak_to_friends}
              disabled={saving}
              onChange={(event) =>
                void save("Show my streak to friends", {
                  show_streak_to_friends: event.target.checked,
                })
              }
            />
            <span>Show my streak to friends</span>
          </label>
        </div>
      )}
      <div role="status" aria-label="Privacy settings status">
        {notice}
      </div>
      {problem && (
        <div role="alert" className="social-error">
          {problem}
        </div>
      )}
    </section>
  );
}
