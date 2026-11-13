import {
  render,
  screen,
  fireEvent,
  waitFor,
  within,
} from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { AddFriend } from "./AddFriend";
import { FriendInvite } from "./FriendInvite";
import { FriendsPage } from "./FriendsPage";
import {
  friendFailure,
  friendMessages,
  inviteUrl,
  parseInviteToken,
} from "./friends";
import { session } from "../accounts/session";

vi.mock("../accounts/session", () => ({ session: { request: vi.fn() } }));
vi.mock("./SocialIdentity", () => ({ SocialIdentity: () => null }));
const request = vi.mocked(session.request);
const rejected = (status: number, code = "") =>
  Object.assign(new Error("server text"), { status, code });
const person = (username: string, extra: object = {}) => ({
  username,
  display_name: `Display ${username}`,
  avatar: { initial: username[0].toUpperCase() },
  ...extra,
});
const friend = (username: string) =>
  person(username, { id: `f-${username}`, since: "2026-11-01T00:00:00Z" });
const incoming = (username: string) =>
  person(username, { id: `r-${username}`, created_at: "2026-11-02T00:00:00Z" });

function serve(
  state: {
    friends?: unknown[];
    incoming?: unknown[];
    outgoing?: unknown[];
  } = {},
  extra: (path: string, method: string) => unknown = () => undefined,
) {
  request.mockImplementation(async (path: string, method = "GET") => {
    const custom = extra(path, method);
    if (custom !== undefined) return custom as never;
    if (path === "/v1/social/friends")
      return { friends: state.friends ?? [] } as never;
    if (path === "/v1/social/friend-requests" && method === "GET")
      return {
        incoming: state.incoming ?? [],
        outgoing: state.outgoing ?? [],
      } as never;
    return { status: "ok" } as never;
  });
}

describe("friend helpers", () => {
  it("classifies failures without trusting server text", () => {
    expect(friendFailure(rejected(404, "request_unavailable"))).toBe(
      "unavailable",
    );
    expect(friendFailure(rejected(404, "invite_unavailable"))).toBe("invite");
    expect(friendFailure(rejected(409, "pending_limit"))).toBe("pending_limit");
    expect(friendFailure(rejected(409, "friend_limit"))).toBe("friend_limit");
    expect(friendFailure(rejected(429))).toBe("limited");
    expect(friendFailure(rejected(503))).toBe("offline");
    expect(friendFailure(new TypeError("Failed to fetch"))).toBe("offline");
    for (const text of Object.values(friendMessages))
      expect(text.toLowerCase()).not.toMatch(/block|private|policy|declin/);
  });
  it("builds and parses invite links", () => {
    const token = "A".repeat(43);
    expect(inviteUrl("https://app.test", token)).toBe(
      `https://app.test/friends/invite/${token}`,
    );
    expect(parseInviteToken(token)).toBe(token);
    expect(parseInviteToken(`${token}x`)).toBeNull();
    expect(parseInviteToken("short")).toBeNull();
    expect(parseInviteToken("bad token ".padEnd(43, "x"))).toBeNull();
  });
});

describe("Friends page", () => {
  beforeEach(() => {
    request.mockReset();
  });

  it("shows loading, then friends and request counts, with only public fields", async () => {
    serve({ friends: [friend("kai")], incoming: [incoming("sam")] });
    render(<FriendsPage />);
    expect(screen.getByText("Loading friends…")).toBeVisible();
    expect(
      await screen.findByRole("tab", { name: "Friends · 1" }),
    ).toBeVisible();
    expect(screen.getByRole("tab", { name: "Requests · 1" })).toBeVisible();
    const list = screen.getByRole("list", { name: "Your friends" });
    expect(within(list).getByText("@kai")).toBeVisible();
    expect(list.textContent).not.toMatch(/@.*@|email|notes/i);
  });

  it("shows the empty state with one call to action", async () => {
    serve();
    render(<FriendsPage />);
    expect(await screen.findByText("Learn with a friend")).toBeVisible();
    expect(
      screen.getAllByRole("button", { name: "Add a friend" }),
    ).toHaveLength(1);
  });

  it("offers retry when loading fails offline", async () => {
    request.mockRejectedValueOnce(new TypeError("Failed to fetch"));
    request.mockRejectedValueOnce(new TypeError("Failed to fetch"));
    render(<FriendsPage />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      friendMessages.offline,
    );
    serve({ friends: [friend("kai")] });
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByText("@kai")).toBeVisible();
  });

  it("accepts and declines requests and cancels sent ones, then reloads", async () => {
    let calls: string[] = [];
    serve(
      {
        incoming: [incoming("sam"), incoming("bea")],
        outgoing: [incoming("ana")],
      },
      (path, method) => {
        if (method !== "GET") {
          calls.push(`${method} ${path}`);
          return { status: "ok" };
        }
        return undefined;
      },
    );
    render(<FriendsPage />);
    fireEvent.click(await screen.findByRole("tab", { name: "Requests · 2" }));
    fireEvent.click(screen.getByRole("button", { name: "Accept sam" }));
    await waitFor(() =>
      expect(calls).toContain("POST /v1/social/friend-requests/r-sam/accept"),
    );
    fireEvent.click(screen.getByRole("button", { name: "Decline bea" }));
    await waitFor(() =>
      expect(calls).toContain("POST /v1/social/friend-requests/r-bea/decline"),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Cancel request to ana" }),
    );
    await waitFor(() =>
      expect(calls).toContain("DELETE /v1/social/friend-requests/r-ana"),
    );
    calls = [];
  });

  it("confirms before removing a friend and restores focus on cancel", async () => {
    const calls: string[] = [];
    serve({ friends: [friend("kai")] }, (path, method) => {
      if (method === "DELETE") {
        calls.push(path);
        return { status: "removed" };
      }
      return undefined;
    });
    render(<FriendsPage />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Open Display kai" }),
    );
    const remove = screen.getByRole("button", { name: "Remove friend" });
    fireEvent.click(remove);
    const dialog = await screen.findByRole("dialog", {
      name: "Remove Display kai?",
    });
    expect(dialog).toHaveTextContent("isn't notified");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(calls).toEqual([]);
    fireEvent.click(screen.getByRole("button", { name: "Remove friend" }));
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Remove",
      }),
    );
    await waitFor(() => expect(calls).toEqual(["/v1/social/friends/f-kai"]));
  });
});

describe("Add a friend sends requests", () => {
  beforeEach(() => {
    request.mockReset();
  });

  it("sends by username, shows the result and a neutral failure", async () => {
    const sends: unknown[] = [];
    request.mockImplementation(async (...args: unknown[]) => {
      const [path, , body] = args as [string, string, unknown];
      if (path.startsWith("/v1/social/search"))
        return { results: [person("mina"), person("milo")] } as never;
      if (path === "/v1/social/friend-link") return { active: false } as never;
      sends.push(body);
      if ((body as { username: string }).username === "milo")
        throw rejected(404, "request_unavailable");
      return { status: "pending", id: "r1" } as never;
    });
    render(<AddFriend onClose={() => {}} />);
    fireEvent.change(screen.getByRole("textbox", { name: "Username" }), {
      target: { value: "mi" + "n" },
    });
    const send = await screen.findByRole("button", {
      name: "Send request to mina",
    });
    fireEvent.click(send);
    expect(await screen.findByText("Request sent")).toBeVisible();
    expect(sends).toContainEqual({ username: "mina" });
    fireEvent.click(
      screen.getByRole("button", { name: "Send request to milo" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      friendMessages.unavailable,
    );
  });

  it("creates and copies an invite link, and can turn it off", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    const token = "B".repeat(43);
    const calls: string[] = [];
    request.mockImplementation(async (path: string, method = "GET") => {
      calls.push(`${method} ${path}`);
      if (path === "/v1/social/friend-link" && method === "POST")
        return { token, expires_at: "2026-12-13T00:00:00Z" } as never;
      if (path === "/v1/social/friend-link" && method === "GET")
        return { active: false } as never;
      return { active: false } as never;
    });
    render(<AddFriend onClose={() => {}} />);
    fireEvent.click(
      await screen.findByRole("button", { name: "Copy invite link" }),
    );
    expect(await screen.findByText("Link copied")).toBeVisible();
    expect(writeText).toHaveBeenCalledWith(inviteUrl(location.origin, token));
    expect(screen.getByText(/Valid for 30 days/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Turn off link" }));
    await waitFor(() =>
      expect(calls).toContain("DELETE /v1/social/friend-link"),
    );
  });
});

describe("Friend invite landing", () => {
  beforeEach(() => {
    request.mockReset();
  });
  const token = "C".repeat(43);

  it("previews the inviter and only creates a request after confirmation", async () => {
    const posts: unknown[] = [];
    request.mockImplementation(async (...args: unknown[]) => {
      const [path, , body] = args as [string, string, unknown];
      if (path === "/v1/social/friend-link/resolve")
        return person("kai") as never;
      posts.push([path, body]);
      return { status: "pending", id: "r" } as never;
    });
    render(<FriendInvite token={token} />);
    expect(await screen.findByText("Display kai")).toBeVisible();
    expect(posts).toEqual([]);
    fireEvent.click(
      screen.getByRole("button", { name: "Send friend request" }),
    );
    expect(await screen.findByText("Request sent")).toBeVisible();
    expect(posts).toEqual([["/v1/social/friend-requests", { token }]]);
  });

  it("shows the neutral expired state and a retry when offline", async () => {
    request.mockRejectedValueOnce(rejected(404, "invite_unavailable"));
    const { unmount } = render(<FriendInvite token={token} />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This invite has expired. Ask for a new one.",
    );
    expect(
      screen.queryByRole("button", { name: "Send friend request" }),
    ).toBeNull();
    unmount();
    request.mockRejectedValueOnce(new TypeError("Failed to fetch"));
    render(<FriendInvite token={token} />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      friendMessages.offline,
    );
    expect(screen.getByRole("button", { name: "Retry" })).toBeVisible();
  });

  it("never calls the API for a malformed token", async () => {
    render(<FriendInvite token="nope" />);
    expect(await screen.findByRole("alert")).toHaveTextContent("expired");
    expect(request).not.toHaveBeenCalled();
  });
});
