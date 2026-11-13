import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { SocialIdentity } from "./SocialIdentity";
import { validateUsername } from "./username";
import { authRequest } from "../accounts/session";

vi.mock("../accounts/session", () => ({
  authRequest: vi.fn(),
  AuthError: class extends Error {
    status: number;
    details: unknown;
    constructor(message: string, status: number, details?: unknown) {
      super(message);
      this.status = status;
      this.details = details;
    }
  },
}));
const neutral = "That username isn't available. Try another.";
const none = { username: null, rules: { min: 3, max: 20 } };
const mine = {
  username: {
    username: "Maya_Learns",
    display_name: "Learner",
    avatar: { initial: "M" },
  },
  rules: { min: 3, max: 20 },
};

describe("username boundaries", () => {
  it.each([
    ["abc", true],
    ["a".repeat(20), true],
    ["Ｍaya", true],
    ["a_b.c1", true],
    ["ab", false],
    ["a".repeat(21), false],
    ["1abc", false],
    ["_abc", false],
    ["abc.", false],
    ["a__b", false],
    ["a._b", false],
    ["a b", false],
    ["ünï", false],
  ])("%s -> %s", (name, valid) => {
    expect(validateUsername(name) === null).toBe(valid);
  });
});

describe("claim a username", () => {
  beforeEach(() => vi.mocked(authRequest).mockReset());
  it("blocks malformed names locally and sends nothing", async () => {
    vi.mocked(authRequest).mockResolvedValueOnce(none);
    render(<SocialIdentity />);
    const input = await screen.findByRole("textbox", { name: "Username" });
    fireEvent.change(input, { target: { value: "a__b" } });
    fireEvent.click(screen.getByRole("button", { name: "Claim username" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "3–20 characters",
    );
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(authRequest).toHaveBeenCalledTimes(1);
  });

  it("shows identical neutral copy for taken and reserved, keeping the draft", async () => {
    vi.mocked(authRequest).mockResolvedValueOnce(none);
    render(<SocialIdentity />);
    const input = await screen.findByRole("textbox", { name: "Username" });
    for (const code of ["username_taken", "username_reserved"]) {
      vi.mocked(authRequest).mockRejectedValueOnce(
        Object.assign(new Error(neutral), { status: 409, details: { code } }),
      );
      fireEvent.change(input, { target: { value: "Support" } });
      fireEvent.click(screen.getByRole("button", { name: "Claim username" }));
      expect(await screen.findByRole("alert")).toHaveTextContent(neutral);
      expect(input).toHaveValue("Support");
    }
    expect(authRequest).toHaveBeenLastCalledWith(
      "/v1/social/username",
      { username: "Support" },
      undefined,
      "PUT",
    );
  });

  it("offers retry on outage and shows the identity card on success", async () => {
    vi.mocked(authRequest).mockResolvedValueOnce(none);
    render(<SocialIdentity />);
    const input = await screen.findByRole("textbox", { name: "Username" });
    fireEvent.change(input, { target: { value: "Maya_Learns" } });
    vi.mocked(authRequest).mockRejectedValueOnce(new Error("Try again."));
    fireEvent.click(screen.getByRole("button", { name: "Claim username" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Try again");
    vi.mocked(authRequest).mockResolvedValueOnce(mine);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    const card = await screen.findByRole("region", {
      name: "Your public identity",
    });
    expect(card).toHaveTextContent("@Maya_Learns");
    expect(card).toHaveTextContent("Learner");
    await waitFor(() =>
      expect(screen.queryByRole("textbox", { name: "Username" })).toBeNull(),
    );
  });

  it("loads an existing identity without the claim form and recovers from load failure", async () => {
    vi.mocked(authRequest).mockRejectedValueOnce(new Error("Try again."));
    render(<SocialIdentity />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Try again");
    vi.mocked(authRequest).mockResolvedValueOnce(mine);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByText("@Maya_Learns")).toBeVisible();
    expect(screen.queryByRole("textbox", { name: "Username" })).toBeNull();
  });

  it("compact mode shows only the handle for Settings", async () => {
    vi.mocked(authRequest).mockResolvedValueOnce(mine);
    render(<SocialIdentity compact />);
    expect(await screen.findByText("@Maya_Learns")).toBeVisible();
  });
});
