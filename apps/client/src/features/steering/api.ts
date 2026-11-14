import { AuthError, session } from "../accounts/session";
export type Instruction = {
  id: string;
  text: string;
  scope: string;
  status: string;
  applied_lesson_id?: string | null;
};
export type InstructionService = {
  latest(courseId: string): Promise<Instruction | null>;
  add(
    courseId: string,
    input: { text: string; scope: string },
  ): Promise<Instruction>;
};
export const instructionApi: InstructionService = {
  async latest(courseId) {
    const r = await session.request<{ instruction: Instruction | null }>(
      `/v1/courses/${encodeURIComponent(courseId)}/instructions/latest`,
    );
    return r.instruction;
  },
  async add(courseId, input) {
    try {
      return await session.request<Instruction>(
        `/v1/courses/${encodeURIComponent(courseId)}/instructions`,
        "POST",
        input,
      );
    } catch (e) {
      if (
        e instanceof AuthError &&
        e.details &&
        typeof e.details === "object" &&
        "reason_code" in e.details
      )
        throw new Error(String(e.details.reason_code), { cause: e });
      throw e;
    }
  },
};
