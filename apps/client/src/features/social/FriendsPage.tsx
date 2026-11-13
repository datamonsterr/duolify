import { SocialIdentity } from "./SocialIdentity";

export function FriendsPage() {
  return (
    <section className="social-page">
      <h1 tabIndex={-1}>Friends</h1>
      <SocialIdentity />
      <p>Friend requests and friend streaks are coming soon.</p>
    </section>
  );
}
