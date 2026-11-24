import { useSyncExternalStore } from "react";

/** A phone-style transient message. One shows at a time; the rest wait in a queue. */
export type ToastOptions = {
  /** Same id replaces a queued or visible toast instead of stacking. */
  id?: string;
  message: string;
  tone?: "info" | "error";
  /** At most one action: Undo, Retry or Settings. */
  action?: { label: string; onAction: () => void };
  ttlMs?: number;
};
export type Toast = ToastOptions & { id: string; tone: "info" | "error" };

export const TOAST_TTL_MS = 4000;
let seq = 0;
let current: Toast | null = null;
let queue: Toast[] = [];
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

export function showToast(options: ToastOptions): string {
  const toast: Toast = {
    ...options,
    id: options.id ?? `toast-${++seq}`,
    tone: options.tone ?? "info",
  };
  if (current?.id === toast.id) current = toast;
  else {
    const at = queue.findIndex((q) => q.id === toast.id);
    if (at >= 0) queue[at] = toast;
    else queue = [...queue, toast];
    if (!current) current = queue.shift() ?? null;
  }
  emit();
  return toast.id;
}

/** Dismiss one toast (or the visible one); the next in the queue shows. */
export function dismissToast(id?: string) {
  if (id && current?.id !== id) queue = queue.filter((q) => q.id !== id);
  else current = queue.shift() ?? null;
  emit();
}

export function resetToasts() {
  current = null;
  queue = [];
  emit();
}

export function useToast(): Toast | null {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => current,
    () => null,
  );
}
