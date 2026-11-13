import { afterEach, expect, it, vi } from "vitest";
import { createSecureStore } from "./secure-store";

const tokens = {
  access_token: "access",
  refresh_token: "refresh",
  expires_in: 900,
};

afterEach(() => {
  sessionStorage.clear();
  localStorage.clear();
});

it("selects the native credential bridge when available", async () => {
  const invoke = vi.fn().mockResolvedValue(JSON.stringify(tokens));
  const store = createSecureStore(invoke);

  await expect(store.read()).resolves.toEqual(tokens);
  expect(invoke).toHaveBeenCalledWith("read_credentials");
});

it("fails closed when native credential storage rejects", async () => {
  sessionStorage.setItem("duolify.auth", JSON.stringify(tokens));
  const invoke = vi.fn().mockRejectedValue(new Error("keychain unavailable"));
  const store = createSecureStore(invoke);

  await expect(store.read()).rejects.toThrow("keychain unavailable");
  await expect(store.write(tokens)).rejects.toThrow("keychain unavailable");
  expect(invoke).toHaveBeenCalledTimes(2);
  expect(sessionStorage.getItem("duolify.auth")).toBe(JSON.stringify(tokens));
  expect(localStorage.length).toBe(0);
});

it("keeps browser tokens in memory and ignores persisted credentials", async () => {
  const store = createSecureStore();
  sessionStorage.setItem(
    "duolify.auth",
    JSON.stringify({ ...tokens, access_token: "stale" }),
  );
  await expect(store.read()).resolves.toBeNull();
  await store.write(tokens);
  await expect(store.read()).resolves.toEqual(tokens);
  await expect(createSecureStore().read()).resolves.toBeNull();
  expect(sessionStorage.getItem("duolify.auth")).not.toBe(
    JSON.stringify(tokens),
  );
  expect(localStorage.length).toBe(0);
});
