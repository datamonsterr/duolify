import { useEffect, useRef } from "react";

/** A ref that always holds the latest value, written in an effect (never during render). */
export function useLatest<T>(value: T) {
  const ref = useRef(value);
  useEffect(() => {
    ref.current = value;
  });
  return ref;
}
