import type { QueryClient } from "@tanstack/react-query";

// Course-tree (["course", id, owner]) and course-list (["courses", ...]) queries use the shared
// stale time, so whatever changes them on the server marks them stale here. Only mounted queries
// refetch at once; the rest refetch when next shown. The shell registers its client at boot
// (like lib/navigation), so non-React code (sync, position saves) can invalidate too.
let client: QueryClient | null = null;

export function registerCourseQueryClient(next: QueryClient | null): void {
  client = next;
}

/** Lesson progress, state or the course outline changed: refresh the tree and the list's progress. */
export function invalidateCourseProgress(): void {
  void client?.invalidateQueries({ queryKey: ["course"] });
  void client?.invalidateQueries({ queryKey: ["courses"] });
}

/** Courses were created, deleted or switched: refresh the list. */
export function invalidateCourseList(): void {
  void client?.invalidateQueries({ queryKey: ["courses"] });
}
