import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { useRouterState } from "@tanstack/react-router";

const freshSession = () => {
  let courseId = "astronomy";
  return {
    courseId: () => courseId,
    rememberCourse: (id: string) => {
      courseId = id;
    },
    slides: new Map<string, number>(),
  };
};
const SessionContext = createContext<ReturnType<typeof freshSession> | null>(
  null,
);

// Disposable per-shell state; durable attempts/drafts remain feature-owned.
export function ShellSession({ children }: { children: ReactNode }) {
  const [session] = useState(freshSession);
  const path = useRouterState({ select: (state) => state.location.pathname });
  useEffect(() => {
    const match = /^\/course\/([^/]+)\/?$/.exec(path);
    if (match) session.rememberCourse(decodeURIComponent(match[1]));
  }, [path, session]);
  return (
    <SessionContext.Provider value={session}>
      {children}
    </SessionContext.Provider>
  );
}
export function useShellSession() {
  const session = useContext(SessionContext);
  const [fallback] = useState(freshSession);
  return session ?? fallback;
}
