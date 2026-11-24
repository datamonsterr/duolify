import { useCallback, useRef, useState } from "react";
import { session } from "../accounts/session";
import { useUiPreferences } from "../design/localization/Preferences";
import { trackGuidedChat } from "../guided-chat/analytics";
import { GuidedScriptChat } from "../guided-chat/GuidedScriptChat";
import { socialAddFriend } from "../guided-chat/social.scripts";
import type { Value } from "../guided-chat/types";
import { useOnline } from "../guided-chat/useOnline";
import {
  friendFailure,
  friendMessages,
  requestsPath,
  type FriendFailure,
  type SendResult,
} from "./friends";
import {
  normalizeSearch,
  searchFailure,
  searchMessages,
  searchPath,
  type FoundLearner,
} from "./search";

/** Add a friend as a chat: type an exact username, confirm, send. Misses stay neutral. */
export function GuidedAddFriend() {
  const { t } = useUiPreferences();
  const online = useOnline();
  const found = useRef("");
  const [sent, setSent] = useState(false);
  const [refused, setRefused] = useState<FriendFailure>();
  const lookup = useCallback(
    async (_step: string, answers: Record<string, Value>) => {
      const query = normalizeSearch(String(answers.username ?? ""));
      try {
        const { results } = await session.request<{ results: FoundLearner[] }>(
          searchPath(query),
        );
        const hit = results.find((r) => r.username.toLowerCase() === query);
        found.current = hit?.username ?? "";
        return hit ? null : t("g.friend.notfound");
      } catch (error) {
        return searchMessages[searchFailure(error)];
      }
    },
    [t],
  );
  const submit = useCallback(async () => {
    setRefused(undefined);
    try {
      await session.request<SendResult>(requestsPath, "POST", {
        username: found.current,
      });
      setSent(true);
    } catch (error) {
      setRefused(friendFailure(error));
      throw error;
    }
  }, []);
  return (
    <section className="guided-friend" aria-label={t("g.friend.title")}>
      <GuidedScriptChat
        script={socialAddFriend}
        online={online}
        onEvent={(event) => trackGuidedChat(socialAddFriend, "friends", event)}
        onStepCommit={lookup}
        onSubmit={submit}
      />
      {sent && <p role="status">{t("g.friend.done")}</p>}
      {refused && (
        <p role="alert" className="social-error">
          {friendMessages[refused]}
        </p>
      )}
    </section>
  );
}
