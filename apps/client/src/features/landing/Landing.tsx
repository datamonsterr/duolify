import { useEffect, useRef, useState, type ReactNode } from "react";
import { Brand } from "../design/brand/Brand";
import { Button } from "../design/Button";
import { Icon, type IconName } from "../design/Icon";
import { useUiPreferences } from "../design/localization/Preferences";
import { trackLanding } from "./analytics";
import { landingMedia } from "./config";
import "./messages.landing";
import "./landing.css";

type CTA = "hero_get_started" | "shared_course" | "header_sign_in";

export function TopBar({ onSignIn }: { onSignIn: () => void }) {
  const { t, preferences, theme, update } = useUiPreferences();
  return (
    <header className="landing-bar">
      <Brand href="/" />
      <nav aria-label={t("landing.nav")}>
        <select
          aria-label={t("landing.language")}
          value={preferences.locale === "ar" ? "ar" : "en"}
          onChange={(event) =>
            update({ locale: event.target.value as "en" | "ar" })
          }
        >
          <option value="en">{t("landing.lang_en")}</option>
          <option value="ar">{t("landing.lang_ar")}</option>
        </select>
        <Button
          variant="quiet"
          className="landing-theme"
          aria-label={t(
            theme === "dark" ? "landing.theme_light" : "landing.theme_dark",
          )}
          onClick={() =>
            update({ appearance: theme === "dark" ? "light" : "dark" })
          }
        >
          <Icon name={theme === "dark" ? "sun" : "moon"} />
        </Button>
        <Button variant="quiet" onClick={onSignIn}>
          {t("landing.sign_in")}
        </Button>
      </nav>
    </header>
  );
}

export function LandingVideo() {
  const { t } = useUiPreferences();
  const video = useRef<HTMLVideoElement>(null);
  const [started, setStarted] = useState(false);
  const [failed, setFailed] = useState(false);
  const playable = Boolean(landingMedia.mp4) && !failed;
  function play() {
    setStarted(true);
    void video.current?.play().catch(() => setFailed(true));
  }
  return (
    <div className="landing-video" data-started={started}>
      <video
        ref={video}
        controls={started && playable}
        preload="none"
        playsInline
        poster={landingMedia.poster}
        aria-label={t("landing.video")}
        onPlay={() => trackLanding("landing_video_played")}
        onEnded={() => trackLanding("landing_video_completed")}
        onError={() => setFailed(true)}
      >
        {playable && (
          <>
            <source
              src={landingMedia.webm}
              type="video/webm"
              onError={() => setFailed(true)}
            />
            <source
              src={landingMedia.mp4}
              type="video/mp4"
              onError={() => setFailed(true)}
            />
          </>
        )}
        <track
          kind="captions"
          srcLang="en"
          label="English"
          src={landingMedia.captionsEn}
          default
        />
      </video>
      {playable && !started && (
        <button
          type="button"
          className="landing-play"
          aria-label={t("landing.video_play")}
          onClick={play}
        >
          <Icon name="play" />
        </button>
      )}
    </div>
  );
}

export function Hero({
  onGetStarted,
  onTryShared,
}: {
  onGetStarted: () => void;
  onTryShared: () => void;
}) {
  const { t } = useUiPreferences();
  return (
    <section className="landing-hero" aria-labelledby="landing-h1">
      <div className="landing-hero-copy">
        <h1 id="landing-h1">{t("landing.h1")}</h1>
        <p>{t("landing.lede")}</p>
        <div className="landing-hero-actions">
          <Button onClick={onGetStarted}>{t("landing.get_started")}</Button>
          <Button variant="quiet" onClick={onTryShared}>
            {t("landing.try_shared")}
          </Button>
        </div>
      </div>
      <LandingVideo />
    </section>
  );
}

const pillars: { icon: IconName; id: "pillar1" | "pillar2" | "pillar3" }[] = [
  { icon: "gem", id: "pillar1" },
  { icon: "ask", id: "pillar2" },
  { icon: "courses", id: "pillar3" },
];
export function Pillars() {
  const { t } = useUiPreferences();
  return (
    <section className="landing-pillars" aria-label={t("landing.pillars")}>
      {pillars.map(({ icon, id }) => (
        <div key={id} className="landing-card">
          <Icon name={icon} />
          <h2>{t(`landing.${id}.title`)}</h2>
          <p>{t(`landing.${id}.body`)}</p>
        </div>
      ))}
    </section>
  );
}

const steps: { icon: IconName; id: "step1" | "step2" | "step3" }[] = [
  { icon: "ask", id: "step1" },
  { icon: "courses", id: "step2" },
  { icon: "goal", id: "step3" },
];
export function Steps() {
  const { t } = useUiPreferences();
  return (
    <section className="landing-steps" aria-labelledby="landing-how">
      <h2 id="landing-how">{t("landing.how")}</h2>
      <ol>
        {steps.map(({ icon, id }, index) => (
          <li key={id}>
            <span className="landing-step-n" aria-hidden="true">
              {index + 1}
            </span>
            <Icon name={icon} />
            <span>{t(`landing.${id}`)}</span>
          </li>
        ))}
      </ol>
    </section>
  );
}

export function Landing({
  welcome,
  footer,
  onOpen,
  onTryShared,
}: {
  /** The shared welcome block (age check, SSO, Get started) and the legal footer. */
  welcome: ReactNode;
  footer: ReactNode;
  /** Opens the sign-in chat; `true` for a returning Learner. */
  onOpen: (returning: boolean) => void;
  onTryShared: () => void;
}) {
  const { t } = useUiPreferences();
  useEffect(() => {
    document.title = t("landing.title");
    document
      .querySelector('meta[name="description"]')
      ?.setAttribute("content", t("landing.description"));
    trackLanding("landing_viewed");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const cta = (target: CTA, action: () => void) => () => {
    trackLanding("landing_cta_clicked", { target });
    action();
  };
  return (
    <div className="landing">
      <TopBar onSignIn={cta("header_sign_in", () => onOpen(true))} />
      <main>
        <Hero
          onGetStarted={cta("hero_get_started", () => onOpen(false))}
          onTryShared={cta("shared_course", onTryShared)}
        />
        <Pillars />
        <Steps />
        <section className="landing-join" aria-labelledby="landing-join">
          <h2 id="landing-join">{t("landing.join")}</h2>
          {welcome}
        </section>
      </main>
      <div className="landing-footer">
        {footer}
        <p className="landing-note">{t("landing.privacy_note")}</p>
      </div>
    </div>
  );
}
