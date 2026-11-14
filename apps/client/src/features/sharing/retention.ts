import { session } from "../accounts/session";

/** Where a copy came from. A deleted owner never leaves a name behind. */
export type CopyOrigin = {
  creator_username?: string | null;
  creator_deleted: boolean;
  source_available: boolean;
};

const KEEPS_WORKING = "This copy keeps working.";

export function originMessage(
  origin?: CopyOrigin | null,
): { title: string; detail: string } | null {
  if (!origin) return null;
  if (origin.creator_deleted) {
    return {
      title: "Created by deleted user",
      detail: `${KEEPS_WORKING} Your progress and lessons are unchanged.`,
    };
  }
  const title = origin.creator_username
    ? `Created by @${origin.creator_username}`
    : "Created by another learner";
  return {
    title,
    detail: origin.source_available
      ? ""
      : `The original course is no longer shared. ${KEEPS_WORKING}`,
  };
}

/** The actor comes from the session; the server decides ownership. */
export const unpublish = (course: string) =>
  session.request<void>(`/v1/courses/${course}/unpublish`, "POST", {});
