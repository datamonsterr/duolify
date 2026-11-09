export const navigation = [
  { id: "learn", label: "Learn", to: "/", icon: "home" },
  { id: "courses", label: "Courses", to: "/courses", icon: "courses" },
  { id: "friends", label: "Friends", to: "/friends", icon: "friends" },
  { id: "leagues", label: "Leagues", to: "/leagues", icon: "flag" },
  { id: "notes", label: "Quick notes", to: "/notes", icon: "note" },
  { id: "bookmarks", label: "Bookmarks", to: "/bookmarks", icon: "bookmark" },
  { id: "credits", label: "Credits", to: "/credits", icon: "gem" },
  { id: "profile", label: "Profile", to: "/profile", icon: "profile" },
  { id: "settings", label: "Settings", to: "/settings", icon: "settings" },
] as const;
export const moreDestinations = navigation.filter((d) =>
  ["leagues", "bookmarks", "credits", "profile", "settings"].includes(d.id),
);
export const mobileTabs = [
  navigation[0],
  navigation[1],
  navigation[2],
  { ...navigation[4], label: "Notes" },
  { id: "more", label: "More", to: "/more", icon: "more" },
] as const;
export type Destination = (typeof navigation)[number]["id"] | "more" | "streak";
export const screenIds = [
  "learn",
  "courses",
  "course_create",
  "friends",
  "leagues",
  "notes",
  "bookmarks",
  "credits",
  "profile",
  "settings",
  "more",
  "streak",
  "lesson",
  "lesson_test",
  "not_found",
] as const;
export type ScreenId = (typeof screenIds)[number];
const at = (path: string, base: string) =>
  path === base || path.startsWith(`${base}/`);
export function currentDestination(
  path: string,
  mobile = false,
): Destination | undefined {
  if (path === "/" || at(path, "/course")) return "learn";
  if (path === "/more") return "more";
  const destination = navigation.find((d) => d.to !== "/" && at(path, d.to));
  if (
    mobile &&
    destination &&
    moreDestinations.some((d) => d.id === destination.id)
  )
    return "more";
  return destination?.id;
}
export function screenForPath(path: string): ScreenId {
  if (/^\/lesson\/[^/]+\/test\/?$/.test(path)) return "lesson_test";
  if (/^\/lesson\/[^/]+\/?$/.test(path)) return "lesson";
  if (path === "/courses/new") return "course_create";
  if (path === "/streak") return "streak";
  return currentDestination(path) ?? "not_found";
}
