import { session } from "../accounts/session";

export type CopyOrigin = {
  creator_username?: string | null;
  creator_deleted: boolean;
  source_available: boolean;
};

export function originMessage(
  _origin?: CopyOrigin | null,
): { title: string; detail: string } | null {
  return null;
}

export const unpublish = (course: string) =>
  session.request<void>(`/v1/courses/${course}/unpublish`, "POST", {});
