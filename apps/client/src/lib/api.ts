export type Lesson = {
  id: string;
  title: string;
  state: "done" | "current" | "locked";
};
export type Course = {
  id: string;
  title: string;
  description: string;
  units: { title: string; lessons: Lesson[] }[];
};
const base = import.meta.env.VITE_API_URL || "http://localhost:8080";
async function get<T>(path: string): Promise<T> {
  const response = await fetch(`${base}${path}`);
  if (!response.ok) throw new Error(`API request failed: ${response.status}`);
  return response.json() as Promise<T>;
}
export const api = {
  courses: () => get<Course[]>("/courses"),
  course: (id: string) => get<Course>(`/courses/${encodeURIComponent(id)}`),
};
