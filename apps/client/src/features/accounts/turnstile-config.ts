import { useCallback, useEffect, useState } from "react";

const apiBase = import.meta.env.VITE_API_URL ?? "http://localhost:8080";
export const turnstileHeader = "X-Turnstile-Token";

/** null means that the client's feature config is still loading. */
export function useTurnstileSiteKey(active: boolean) {
  const [generation, setGeneration] = useState(0);
  const [config, setConfig] = useState<{
    generation: number;
    siteKey: string;
  }>();
  const reload = useCallback(
    () => setGeneration((previous) => previous + 1),
    [],
  );
  useEffect(() => {
    if (!active) return;
    let live = true;
    const controller = new AbortController();
    const timer = window.setTimeout(() => controller.abort(), 3000);
    void fetch(`${apiBase}/v1/auth/turnstile/config`, {
      cache: "no-store",
      credentials: "omit",
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) return "";
        const config: unknown = await response.json();
        if (
          config &&
          typeof config === "object" &&
          "enabled" in config &&
          config.enabled === true &&
          "site_key" in config &&
          typeof config.site_key === "string"
        ) {
          return config.site_key;
        }
        return "";
      })
      .catch(() => "")
      .then((key) => {
        window.clearTimeout(timer);
        if (live) setConfig({ generation, siteKey: key });
      });
    return () => {
      live = false;
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [active, generation]);
  return {
    siteKey: config?.generation === generation ? config.siteKey : null,
    reload,
  };
}

export function isTurnstileFailure(details: unknown) {
  return Boolean(
    details &&
    typeof details === "object" &&
    "code" in details &&
    details.code === "turnstile_failed",
  );
}
