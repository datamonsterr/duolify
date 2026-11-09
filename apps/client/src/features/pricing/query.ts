import { queryOptions } from "@tanstack/react-query";
import { api, readPricingCache } from "../../lib/api";
import type { Pricing } from "./types";

export const pricingQueryKey = ["pricing"] as const;

export function pricingQueryOptions() {
  const cached = readPricingCache();
  return queryOptions<Pricing>({
    queryKey: pricingQueryKey,
    queryFn: api.pricing,
    initialData: cached?.pricing,
    staleTime: 0,
    retry: false,
    networkMode: "always",
  });
}
