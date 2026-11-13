import { describeStreak, sideStatus, type FriendStreak } from "./friendStreak";

/** One compact line group under a friend row: shared length and both local-day statuses. */
export function FriendStreakRow({
  name,
  streak,
}: {
  name: string;
  streak: FriendStreak;
}) {
  const text = describeStreak(name, streak);
  const live = streak.state !== "none" && streak.state !== "broken";
  return (
    <p className="friend-streak" data-state={streak.state}>
      <span className="friend-streak-head">{text.headline}</span>
      {live && (
        <>
          <span>{text.you}</span>
          <span>
            <bdi>{name}</bdi>: {sideStatus(streak.friend)}
          </span>
        </>
      )}
      {text.note && <span className="friend-streak-note">{text.note}</span>}
    </p>
  );
}

function Day({
  who,
  side,
}: {
  who: React.ReactNode;
  side: FriendStreak["you"];
}) {
  return (
    <li>
      <strong>{who}</strong>
      <span>{sideStatus(side)}</span>
      <span>
        {side.local_date} · {side.time_zone}
      </span>
    </li>
  );
}

/** Detail: each friend's own local day; nothing is finalised on the viewer's midnight. */
export function FriendStreakDetail({
  name,
  streak,
}: {
  name: string;
  streak: FriendStreak;
}) {
  const text = describeStreak(name, streak);
  const live = streak.state !== "none" && streak.state !== "broken";
  return (
    <section className="friend-streak-detail" aria-label="Friend streak">
      <h3>{live ? `${streak.days}-day friend streak` : text.headline}</h3>
      <ul className="friend-streak-days" aria-label="Local days">
        <Day who="You" side={streak.you} />
        <Day who={<bdi>{name}</bdi>} side={streak.friend} />
      </ul>
      {text.note && <p role="status">{text.note}</p>}
      {live && streak.state.startsWith("waiting") && (
        <p className="social-hint" dir="auto">
          {streak.state === "waiting_you"
            ? "Today counts once you finish a lesson."
            : `Today counts once ${name} finishes before their day ends.`}
        </p>
      )}
      {!live && (
        <p className="social-hint">
          A shared day grows when you both finish a lesson.
        </p>
      )}
    </section>
  );
}
