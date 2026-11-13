import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { useRouterState } from "@tanstack/react-router";
import { api } from "../../lib/api";

const freshSession = () => {
  let courseId = (() => {
    try {
      return localStorage.getItem("duolify:active-course") || "astronomy";
    } catch {
      return "astronomy";
    }
  })();

  return {
    courseId: () => courseId,
    rememberCourse: (id: string, syncServer = true) => {
      courseId = id;
      try {
        localStorage.setItem("duolify:active-course", id);
      } catch {
        /* LocalStorage may be unavailable. */
      }
      if (syncServer && /^[0-9a-f-]{36}$/.test(id)) {
        void api.selectCourse(id).catch(() => {});
      }
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
    if (match) {
      session.rememberCourse(decodeURIComponent(match[1]));
    }
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
