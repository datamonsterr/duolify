// Per-script rollout flags. The old form stays one release behind each flag for rollback.
// Default is on (dev, e2e, staging). `VITE_FEATURE_GUIDED_<NAME>=false` turns a script off,
// `ab` serves it to half of the Learners by id. A local override (`duolify.guided.<name>` =
// `on` or `off`) lets QA and the rollback tests pick a side.
export type GuidedFlag =
  | "onboarding"
  | "course_brief"
  | "course_level"
  | "course_steer"
  | "signin"
  | "character_create"
  | "character_cast"
  | "rest";

const env = import.meta.env as Record<string, string | undefined>;

function bucket(id: string) {
  let h = 0;
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return h % 2 === 0;
}

export function guidedEnabled(flag: GuidedFlag, learnerId = ""): boolean {
  try {
    const local = localStorage.getItem(`duolify.guided.${flag}`);
    if (local === "on") return true;
    if (local === "off") return false;
  } catch {
    /* storage may be unavailable */
  }
  const value = env[`VITE_FEATURE_GUIDED_${flag.toUpperCase()}`];
  if (value === "false") return false;
  if (value === "ab") return learnerId ? bucket(learnerId) : false;
  return true;
}
