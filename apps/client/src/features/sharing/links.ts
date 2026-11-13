import { session } from "../accounts/session";
import type { PublicationDraft } from "./drafts";

export type LinkState = {
  visibility: "private" | "link" | "public";
  active: boolean;
  version?: number;
  lesson_count?: number;
  created_at?: string;
};
export type LinkCreated = {
  token: string;
  preview_url: string;
  deep_link: string;
};
export type PublicPreview = {
  course_id: string;
  title: string;
  description: string;
  version: number;
  lesson_count: number;
  generated_lesson_count: number;
  creator: { username: string; display_name: string; avatar_url?: string };
  outline: {
    title: string;
    lessons: { title: string; generated: boolean }[];
  }[];
  sample_slides: { id: string; html: string }[];
  enrollment_count: number;
};
export type LinkFailure = "unavailable" | "offline" | "rate_limited";
export type PublishResult =
  | { kind: "published"; link: LinkCreated; version: number; lessons: number }
  | { kind: "blocked"; verdict: PublicationDraft["verdict"] };

const apiBase = import.meta.env.VITE_API_URL || "http://localhost:8080";
const root = (course: string) => `/v1/courses/${course}/share-link`;

/** The actor always comes from the session; the server decides ownership. */
export const links = {
  status: (course: string) => session.request<LinkState>(root(course)),
  /** Screens a fresh draft; only verdict "pass" is published. */
  async publish(course: string): Promise<PublishResult> {
    const draft = await session.request<PublicationDraft>(
      `/v1/courses/${course}/publication-drafts`,
      "POST",
      {},
    );
    if (draft.verdict !== "pass")
      return { kind: "blocked", verdict: draft.verdict };
    const link = await session.request<LinkCreated>(root(course), "POST", {});
    return {
      kind: "published",
      link,
      version: draft.number,
      lessons: draft.lesson_count,
    };
  },
  rotate: (course: string) =>
    session.request<LinkCreated>(`${root(course)}/rotate`, "POST", {}),
  revoke: (course: string) => session.request<void>(root(course), "DELETE"),
};

/** Anonymous read: no credentials, so a stale session can never change the answer. */
export async function fetchPreview(token: string): Promise<PublicPreview> {
  const response = await fetch(
    `${apiBase}/v1/public/shares/${encodeURIComponent(token)}`,
    { credentials: "omit", headers: { Accept: "application/json" } },
  );
  if (!response.ok)
    throw Object.assign(new Error("preview unavailable"), {
      status: response.status,
    });
  return (await response.json()) as PublicPreview;
}

/** Shows the host and the ends of the token, like the canvas `duolify.app/c/Hq7…x2`. */
export function shortLink(url: string): string {
  const match = /^https?:\/\/([^/]+)\/c\/([A-Za-z0-9_-]+)$/.exec(url);
  if (!match) return "…";
  const [, host, token] = match;
  return `${host}/c/${token!.slice(0, 3)}…${token!.slice(-2)}`;
}

/** Classifies by status only, never by server text; 404 is the one neutral refusal. */
export function linkFailure(error: unknown): LinkFailure {
  const status = (error as { status?: number }).status;
  if (status === 404) return "unavailable";
  if (status === 429) return "rate_limited";
  return "offline";
}

export const linkMessages: Record<LinkFailure, string> = {
  unavailable: "This course isn't available to share.",
  offline: "Couldn't reach Duolify. Check your connection and retry.",
  rate_limited: "Too many requests. Wait a minute, then retry.",
};
