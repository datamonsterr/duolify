// Design-review page: all states and sizes. Query: theme=light|dark, dir=rtl, still=1, swatch, acc=a,b, only=<state>, size=<px>.
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "../../styles.css";
import { UiPreferencesProvider } from "../../features/design/localization/Preferences";
import {
  Mascot,
  MascotAvatar,
  PlayerCompanion,
  emitMascotCue,
  emitMascotTalk,
} from "./index";
import {
  MASCOT_STATES,
  type MascotAccessory,
  type MascotSwatch,
} from "./rig-part-spec";

const q = new URLSearchParams(location.search);
const theme = q.get("theme") === "dark" ? "dark" : "light";
const appearance = {
  swatch: (q.get("swatch") as MascotSwatch) || undefined,
  accessories: (q.get("acc")?.split(",").filter(Boolean) ??
    []) as MascotAccessory[],
};
const only = q.get("only");
const big = Number(q.get("size") ?? 120);
document.documentElement.dir = q.get("dir") === "rtl" ? "rtl" : "ltr";

const scene = q.get("scene");
const Shell = ({ children }: { children: React.ReactNode }) => (
  <main
    style={{
      background: "var(--canvas)",
      color: "var(--ink, var(--text))",
      minHeight: "100vh",
      padding: 16,
      fontFamily: "system-ui",
    }}
  >
    {children}
  </main>
);
// Stand-ins for placements that need the full app: framed player chrome and empty/error/offline cards.
const Scenes = () =>
  scene === "player" ? (
    <Shell>
      <header
        className="focused-context"
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
        }}
      >
        <a href="#exit">Exit lesson</a>
        <span>Course · Lesson</span>
        <PlayerCompanion immersive={q.get("immersive") === "1"} />
      </header>
      <div
        data-testid="slide"
        style={{
          marginTop: 12,
          aspectRatio: "16/9",
          background: "var(--surface)",
          border: "2px solid var(--border, #ddd)",
          borderRadius: 16,
        }}
      />
      <button onClick={() => emitMascotCue("celebrate")}>Pass</button>
      <button onClick={() => emitMascotTalk(true)}>Hint</button>
    </Shell>
  ) : (
    <Shell>
      <div style={{ display: "grid", gap: 16 }}>
        {(["idle", "confused", "sleepy"] as const).map((s) => (
          <section
            key={s}
            data-s={s}
            style={{
              textAlign: "center",
              background: "var(--surface)",
              borderRadius: 16,
              padding: 16,
            }}
          >
            <Mascot
              state={s}
              size={96}
              offline={s === "sleepy"}
              appearance={appearance}
              labelled={s !== "idle"}
            />
            <p>
              {s === "idle"
                ? "Nothing here yet"
                : s === "confused"
                  ? "Something went wrong"
                  : "You're offline"}
            </p>
          </section>
        ))}
      </div>
    </Shell>
  );

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <UiPreferencesProvider
      initialPreferences={{
        appearance: theme,
        reduceMotion: q.get("still") === "1",
      }}
    >
      {scene ? (
        <Scenes />
      ) : (
        <main
          style={{
            background: "var(--canvas)",
            color: "var(--ink, var(--text))",
            minHeight: "100vh",
            padding: 16,
            fontFamily: "system-ui",
          }}
        >
          <div
            id="sheet"
            style={{
              display: "grid",
              gap: 16,
              gridTemplateColumns: `repeat(auto-fill,minmax(${big + 40}px,1fr))`,
            }}
          >
            {MASCOT_STATES.filter((s) => !only || s === only).map((s) => (
              <figure
                key={s}
                data-s={s}
                style={{
                  margin: 0,
                  textAlign: "center",
                  background: "var(--surface)",
                  borderRadius: 16,
                  padding: 12,
                  border: "2px solid var(--border, #e5e5e5)",
                }}
              >
                <Mascot
                  state={s}
                  size={big}
                  appearance={appearance}
                  offline={s === "sleepy"}
                  label={s}
                />
                <div
                  style={{
                    display: "flex",
                    gap: 10,
                    justifyContent: "center",
                    alignItems: "center",
                    margin: "8px 0",
                    flexWrap: "wrap",
                  }}
                >
                  <Mascot
                    state={s}
                    size={64}
                    appearance={appearance}
                    view="full"
                  />
                  <Mascot state={s} size={32} appearance={appearance} />
                  <MascotAvatar state={s} size={32} appearance={appearance} />
                  <MascotAvatar state={s} size={48} appearance={appearance} />
                </div>
                <figcaption style={{ fontWeight: 800 }}>{s}</figcaption>
              </figure>
            ))}
          </div>
        </main>
      )}
    </UiPreferencesProvider>
  </StrictMode>,
);
