import { useCallback, useEffect, useRef, useState } from "react";
import { session } from "../accounts/session";
import type { WalletResponse } from "../credits/types";
import {
  TutorApiError,
  TutorStreamError,
  type ReplyClass,
  type TutorQuote,
  type ThreadResponse,
  type TutorService,
} from "./tutor";

export type ChatMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  status: "complete" | "interrupted" | "stopped" | "streaming";
  /** Typed end reason of an interrupted reply, e.g. `idle_timeout`. */
  reason?: string;
};
export type Notice =
  | { kind: "credits"; nextGrantAt?: string }
  | { kind: "failed" }
  | { kind: "stale" }
  | { kind: "stopped" }
  | { kind: "load" };
export type Phase = "idle" | "sending" | "streaming";
type QuoteState = {
  quote: TutorQuote;
  cls: ReplyClass;
  message: string;
};
type Pending = { key: string; quote: QuoteState; sameSend: boolean };

const quoteDelay = 300;
const draftKey = (courseId: string) =>
  `tutor-draft:${session.current()?.id ?? ""}:${courseId}`;
function readDraft(courseId: string) {
  try {
    return sessionStorage.getItem(draftKey(courseId)) ?? "";
  } catch {
    return "";
  }
}
function writeDraft(courseId: string, value: string) {
  try {
    if (value) sessionStorage.setItem(draftKey(courseId), value);
    else sessionStorage.removeItem(draftKey(courseId));
  } catch {
    /* The mounted composer keeps the text when storage is unavailable. */
  }
}
export function lastIndex<T>(items: T[], test: (item: T) => boolean) {
  for (let i = items.length - 1; i >= 0; i--) if (test(items[i])) return i;
  return -1;
}
const fresh = (q?: QuoteState) =>
  !!q && Date.parse(q.quote.expires_at) > Date.now() + 5000;
let localId = 0;
const nextId = () => `local-${++localId}`;

export function useTutor(courseId: string, service: TutorService, on: boolean) {
  const [draft, setDraftState] = useState(() => readDraft(courseId));
  const [cls, setCls] = useState<ReplyClass>("short");
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [threadId, setThreadId] = useState<string>();
  const [wallet, setWallet] = useState<WalletResponse>();
  const [quote, setQuote] = useState<QuoteState>();
  const [settled, setSettled] = useState<string>();
  const [phase, setPhase] = useState<Phase>("idle");
  const [notice, setNotice] = useState<Notice>();
  const [online, setOnline] = useState(() => navigator.onLine);
  const [loaded, setLoaded] = useState(false);
  const abort = useRef<AbortController | null>(null);
  const pending = useRef<Pending | null>(null);
  const lock = useRef(false);
  const keyRef = useRef<string | undefined>(undefined);
  const quoteSeq = useRef(0);
  const threadRef = useRef<string | undefined>(undefined);
  useEffect(() => {
    threadRef.current = threadId;
  }, [threadId]);

  useEffect(() => {
    const up = () => setOnline(true);
    const down = () => setOnline(false);
    window.addEventListener("online", up);
    window.addEventListener("offline", down);
    return () => {
      window.removeEventListener("online", up);
      window.removeEventListener("offline", down);
    };
  }, []);

  const refreshWallet = useCallback(async () => {
    try {
      setWallet(await service.wallet());
    } catch {
      /* The last known balance stays on screen. */
    }
  }, [service]);
  const applyThread = useCallback((response: ThreadResponse) => {
    setThreadId(response.thread?.id);
    setMessages(
      response.messages.map((m) => ({
        id: m.id,
        role: m.role,
        content: m.content,
        status: m.status,
      })),
    );
    setNotice((n) => (n?.kind === "load" ? undefined : n));
    setLoaded(true);
  }, []);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!on) return;
    let live = true;
    service.wallet().then(
      (value) => {
        if (live) setWallet(value);
      },
      () => {
        /* The last known balance stays on screen. */
      },
    );
    return () => {
      live = false;
    };
  }, [on, service]);
  useEffect(() => {
    if (!on || loaded) return;
    let live = true;
    service.thread(courseId).then(
      (response) => {
        if (live) applyThread(response);
      },
      () => {
        if (live) setNotice({ kind: "load" });
      },
    );
    return () => {
      live = false;
    };
  }, [on, loaded, attempt, courseId, service, applyThread]);

  const setDraft = (value: string) => {
    setDraftState(value);
    writeDraft(courseId, value);
  };

  // One estimate per (class, message). It is requested while the learner types,
  // and replaced whenever either changes or it expires.
  const text = draft.trim();
  useEffect(() => {
    if (!on || phase !== "idle" || !online || !text) {
      quoteSeq.current++;
      return;
    }
    if (quote && quote.cls === cls && quote.message === text && fresh(quote))
      return;
    const seq = ++quoteSeq.current;
    const timer = setTimeout(() => {
      service
        .quote(courseId, cls, text)
        .then((q) => {
          if (seq === quoteSeq.current)
            setQuote({ quote: q, cls, message: text });
        })
        .catch(() => {
          if (seq === quoteSeq.current) setQuote(undefined);
        })
        .finally(() => {
          if (seq === quoteSeq.current) setSettled(`${cls}:${text}`);
        });
    }, quoteDelay);
    return () => clearTimeout(timer);
  }, [on, phase, online, text, cls, courseId, service, quote]);

  const current =
    quote && quote.cls === cls && quote.message === text && fresh(quote)
      ? quote
      : undefined;

  const quoting =
    on &&
    online &&
    phase === "idle" &&
    !!text &&
    !current &&
    settled !== `${cls}:${text}`;
  const loading = on && !loaded && phase === "idle" && notice?.kind !== "load";

  const deliver = useCallback(
    async (message: string, reply: ReplyClass, retry?: Pending) => {
      if (lock.current) return;
      lock.current = true;
      setPhase("sending");
      setNotice(undefined);
      const controller = new AbortController();
      abort.current = controller;
      const assistantId = nextId();
      const userId = nextId();
      let opened = false;
      let received = "";
      const update = (patch: Partial<ChatMessage>) =>
        setMessages((all) =>
          all.map((m) => (m.id === assistantId ? { ...m, ...patch } : m)),
        );
      let attempt: Pending | undefined = retry;
      try {
        if (!attempt) {
          const state: QuoteState =
            current && current.cls === reply && current.message === message
              ? current
              : {
                  quote: await service.quote(courseId, reply, message),
                  cls: reply,
                  message,
                };
          attempt = { key: crypto.randomUUID(), quote: state, sameSend: false };
        }
        pending.current = attempt;
        keyRef.current = attempt.key;
        // The Send click is the consent to this quote.
        await service.accept(attempt.quote.quote.id);
        const done = await service.send(
          courseId,
          {
            quote_id: attempt.quote.quote.id,
            idempotency_key: attempt.key,
            class: reply,
            message,
            ...(threadRef.current ? { thread_id: threadRef.current } : {}),
          },
          {
            onOpen: () => {
              opened = true;
              setPhase("streaming");
              setMessages((all) => [
                ...all,
                {
                  id: userId,
                  role: "user",
                  content: message,
                  status: "complete",
                },
                {
                  id: assistantId,
                  role: "assistant",
                  content: "",
                  status: "streaming",
                },
              ]);
              if (readDraft(courseId).trim() === message) setDraft("");
            },
            onThread: (id) => setThreadId(id),
            onDelta: (delta) => {
              received += delta;
              update({ content: received });
            },
          },
          controller.signal,
        );
        setThreadId(done.thread_id);
        update({ id: done.message_id, status: "complete" });
        pending.current = null;
        setQuote(undefined);
      } catch (error) {
        const aborted =
          controller.signal.aborted ||
          (error instanceof DOMException && error.name === "AbortError");
        if (opened) {
          const typed = error instanceof TutorStreamError ? error : undefined;
          const cut = !typed || typed.interrupted;
          if (received && (aborted || cut)) {
            // The server stored the prefix: take its id, text and status as the truth.
            const stored = attempt
              ? await service.turn(courseId, attempt.key)
              : null;
            const status = aborted ? "stopped" : "interrupted";
            update({
              status: stored?.message?.status ?? status,
              reason: typed?.code ?? stored?.end_reason,
              ...(stored?.message
                ? { id: stored.message.id, content: stored.message.content }
                : {}),
            });
            if (aborted) setNotice({ kind: "stopped" });
          } else {
            setMessages((all) =>
              all.filter((m) => m.id !== assistantId && m.id !== userId),
            );
            setDraft(message);
            setNotice({ kind: aborted ? "stopped" : "failed" });
          }
          pending.current = null;
        } else if (aborted) {
          // Stopped before the first token: nothing was stored or charged and the draft is kept.
          setNotice({ kind: "stopped" });
          pending.current = null;
        } else if (error instanceof TutorApiError && error.status === 402) {
          setNotice({
            kind: "credits",
            nextGrantAt: error.details.next_grant_at,
          });
          pending.current = null;
        } else if (error instanceof TutorApiError && error.status === 409) {
          setQuote(undefined);
          setNotice({ kind: "stale" });
          pending.current = null;
        } else {
          // A dropped connection before the stream opened may still have started
          // the turn: its key is reused so the retry replays instead of charging.
          const network = !(error instanceof TutorApiError);
          if (network && attempt)
            pending.current = { ...attempt, sameSend: true };
          else pending.current = null;
          setNotice({ kind: "failed" });
        }
        setQuote((q) => (pending.current ? q : undefined));
      } finally {
        lock.current = false;
        abort.current = null;
        setPhase("idle");
        void refreshWallet();
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [courseId, service, current, refreshWallet],
  );

  const send = () => {
    if (!text || phase !== "idle" || !online) return;
    const retry =
      pending.current?.sameSend &&
      pending.current.quote.message === text &&
      pending.current.quote.cls === cls
        ? pending.current
        : undefined;
    void deliver(text, cls, retry);
  };
  const askAgain = () => {
    const index = lastIndex(messages, (m) => m.status === "interrupted");
    const before = messages.slice(0, Math.max(index, 0));
    const prompt = before[lastIndex(before, (m) => m.role === "user")]?.content;
    if (!prompt || phase !== "idle" || !online) return;
    pending.current = null;
    setQuote(undefined);
    void deliver(prompt, cls);
  };
  const stop = async () => {
    const controller = abort.current;
    if (!controller) return;
    // Ask the server first so the stop is never mistaken for a disconnect, then drop the connection.
    if (keyRef.current)
      await service.stop(courseId, keyRef.current).catch(() => undefined);
    controller.abort();
  };
  const remove = async () => {
    if (!threadId) return;
    await service.remove(threadId);
    setThreadId(undefined);
    setMessages([]);
    setNotice(undefined);
    pending.current = null;
  };
  return {
    draft,
    setDraft,
    cls,
    setCls,
    messages,
    threadId,
    wallet,
    quote: current,
    quoting,
    phase,
    notice,
    loading,
    online,
    send,
    askAgain,
    stop,
    remove,
    reload: () => {
      setNotice(undefined);
      setAttempt((n) => n + 1);
    },
  };
}
