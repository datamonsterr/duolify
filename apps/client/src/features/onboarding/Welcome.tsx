import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "../design/Button";
import { useUiPreferences } from "../design/localization/Preferences";
import { navigateTo } from "../../lib/navigation";
import { trackOnboarding } from "../../lib/analytics";
import { enrollExample, listExamples, type Example } from "./api";
import { handoffTopic } from "./handoff";
import { Modal } from "./Modal";

export function Welcome({
  owner,
  onFinish,
}: {
  owner: string;
  onFinish: (state: "completed" | "skipped", tour: boolean) => void;
}) {
  const { t, number } = useUiPreferences();
  const [topic, setTopic] = useState("");
  const [examples, setExamples] = useState<Example[]>([]);
  const [showExamples, setShowExamples] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const queries = useQueryClient();
  useEffect(() => {
    trackOnboarding({ event: "onboarding_welcome_shown" });
    let active = true;
    void listExamples().then((value) => {
      if (active) setExamples(value);
    });
    return () => {
      active = false;
    };
  }, []);
  function choose(path: "topic" | "setup_chat" | "skip") {
    if (path === "topic") {
      if (!topic.trim()) return;
      try {
        handoffTopic(owner, topic);
      } catch {
        setError(t("onboarding.topic_error"));
        return;
      }
    }
    trackOnboarding({ event: "onboarding_path_chosen", path });
    onFinish(path === "skip" ? "skipped" : "completed", path === "skip");
    if (path !== "skip")
      navigateTo(path === "topic" ? "/courses/create" : "/profile/setup");
  }
  async function enroll(example: Example) {
    setPending(true);
    setError("");
    try {
      const { course_id } = await enrollExample(example.slug);
      await queries.invalidateQueries({ queryKey: ["courses"] });
      trackOnboarding({ event: "onboarding_path_chosen", path: "example" });
      onFinish("completed", false);
      navigateTo(`/course/${encodeURIComponent(course_id)}`);
    } catch {
      setError(t("onboarding.enroll_error"));
      setPending(false);
    }
  }
  return (
    <Modal
      label={t("onboarding.welcome")}
      onSkip={() => choose("skip")}
      testId="onboarding-welcome"
    >
      <section className="onboarding-welcome-card">
        <div className="onboarding-tiles" aria-hidden="true">
          <span />
          <span />
        </div>
        <p className="onboarding-eyebrow">{t("onboarding.welcome")}</p>
        <h2 tabIndex={-1}>{t("onboarding.question")}</h2>
        <p>{t("onboarding.intro")}</p>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            choose("topic");
          }}
        >
          <label htmlFor="onboarding-topic">{t("onboarding.question")}</label>
          <input
            id="onboarding-topic"
            autoComplete="off"
            maxLength={2000}
            value={topic}
            placeholder={t("onboarding.topic_hint")}
            onChange={(event) => setTopic(event.target.value)}
          />
          <Button type="submit" disabled={!topic.trim() || pending}>
            {t("onboarding.topic_go")}
          </Button>
        </form>
        <div className="onboarding-choices">
          <Button
            variant="secondary"
            disabled={pending}
            onClick={() => choose("setup_chat")}
          >
            {t("onboarding.decide")}
          </Button>
          {examples.length > 0 && (
            <Button
              variant="quiet"
              disabled={pending}
              aria-expanded={showExamples}
              onClick={() => setShowExamples(!showExamples)}
            >
              {t("onboarding.example")}
            </Button>
          )}
        </div>
        {showExamples && (
          <section
            aria-label={t("onboarding.examples_title")}
            className="onboarding-examples"
          >
            <h3>{t("onboarding.examples_title")}</h3>
            <p>{t("onboarding.example_free")}</p>
            {examples.map((example) => (
              <Button
                key={example.slug}
                variant="quiet"
                disabled={pending}
                onClick={() => void enroll(example)}
              >
                <strong dir="auto">{example.title}</strong>
                <span dir="auto">{example.description}</span>
                <small>
                  {t("onboarding.example_detail", {
                    units: number(example.units),
                    lessons: number(example.lessons),
                    minutes: number(example.estimated_minutes),
                  })}
                </small>
              </Button>
            ))}
          </section>
        )}
        {error && <p role="alert">{error}</p>}
        <Button
          variant="quiet"
          className="onboarding-skip"
          onClick={() => choose("skip")}
        >
          {t("onboarding.skip_welcome")}
        </Button>
        <p className="onboarding-privacy">{t("onboarding.privacy")}</p>
      </section>
    </Modal>
  );
}
