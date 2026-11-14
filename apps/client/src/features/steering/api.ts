import { AuthError, session } from "../accounts/session";
export type Instruction = {
  id: string;
  text: string;
  scope: string;
  status: string;
  version?: number;
  added_at?: string;
  applied_lesson_id?: string | null;
  delivery?: "ready" | "running" | "waits" | "";
};
export type InstructionLimits = {
  max_characters: number;
  max_active: number;
  max_total_characters: number;
  adds_per_hour: number;
};
// Set only for a published course copy (E16-05).
export type ForkInfo = {
  required: boolean;
  forked: boolean;
  kept_lessons: number;
};
export type InstructionList = {
  instructions: Instruction[];
  limits: InstructionLimits;
  active_count: number;
  active_characters: number;
  fork?: ForkInfo;
};
export type InstructionService = {
  latest(courseId: string): Promise<Instruction | null>;
  list(courseId: string): Promise<InstructionList>;
  add(
    courseId: string,
    input: { text: string; scope: string; confirm_fork?: boolean },
  ): Promise<Instruction>;
  edit(
    courseId: string,
    id: string,
    input: { expected_version: number; text?: string; scope?: string },
  ): Promise<Instruction>;
  remove(courseId: string, id: string): Promise<void>;
};
const base = (courseId: string) =>
  `/v1/courses/${encodeURIComponent(courseId)}/instructions`;
// Surfaces the API reason code as the error message so the UI can name it.
async function coded<T>(call: () => Promise<T>): Promise<T> {
  try {
    return await call();
  } catch (e) {
    if (e instanceof AuthError) {
      const d = e.details;
      if (d && typeof d === "object" && "reason_code" in d)
        throw new Error(String(d.reason_code), { cause: e });
      if (e.status === 404) throw new Error("not_found", { cause: e });
    }
    throw e;
  }
}
export const instructionApi: InstructionService = {
  async latest(courseId) {
    const r = await session.request<{ instruction: Instruction | null }>(
      `${base(courseId)}/latest`,
    );
    return r.instruction;
  },
  list: (courseId) => session.request<InstructionList>(base(courseId)),
  add: (courseId, input) =>
    coded(() => session.request<Instruction>(base(courseId), "POST", input)),
  edit: (courseId, id, input) =>
    coded(() =>
      session.request<Instruction>(
        `${base(courseId)}/${encodeURIComponent(id)}`,
        "PATCH",
        input,
      ),
    ),
  async remove(courseId, id) {
    await coded(() =>
      session.request(`${base(courseId)}/${encodeURIComponent(id)}`, "DELETE"),
    );
  },
};
