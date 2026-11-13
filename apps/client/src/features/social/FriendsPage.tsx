import { useState } from "react";
import { AddFriend } from "./AddFriend";
import { SocialIdentity } from "./SocialIdentity";

export function FriendsPage() {
  const [adding, setAdding] = useState(false);
  return (
    <section className="social-page">
      <h1 tabIndex={-1}>Friends</h1>
      <SocialIdentity />
      <div className="social-actions">
        <button
          type="button"
          className="duo-button primary"
          onClick={() => setAdding(true)}
        >
          Add
        </button>
      </div>
      {adding && <AddFriend onClose={() => setAdding(false)} />}
      <p>Friend requests and friend streaks are coming soon.</p>
    </section>
  );
}
