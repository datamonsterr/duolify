import { useEffect, useMemo, useRef, useState } from "react";
import {
  catalogueUrl,
  categories,
  fetchCatalogue,
  languages,
  learnersLabel,
  marketFailure,
  marketMessages,
  ratingLabel,
  sorts,
  type BrowseParams,
  type CatalogueItem,
  type MarketFailure,
  type Sort,
} from "./market";
import "./sharing.css";
import "./market.css";

type Result = {
  key: string;
  items: CatalogueItem[];
  next: string;
  failure?: MarketFailure;
};

const sortIds = sorts.map((s) => s.id) as string[];

/** Filters live in the URL, so Back from a Course page returns to the same results. */
function fromLocation(): Required<Omit<BrowseParams, "cursor">> {
  const p = new URLSearchParams(window.location.search);
  const sort = p.get("sort") ?? "";
  return {
    q: p.get("q") ?? "",
    category: p.get("category") ?? "",
    language: p.get("language") ?? "",
    sort: (sortIds.includes(sort) ? sort : "popular") as Sort,
  };
}

const creatorName = (item: CatalogueItem) =>
  item.creator.username
    ? `@${item.creator.username}`
    : item.creator.display_name || "a Duolify creator";

function Card({ item }: { item: CatalogueItem }) {
  return (
    <article className="market-card">
      <h2 dir="auto">
        <a href={`/marketplace/${item.course_id}`}>{item.title}</a>
      </h2>
      <p className="market-meta">
        {creatorName(item)} · {ratingLabel(item)} ·{" "}
        {learnersLabel(item.enrollment_count)}
      </p>
      <p className="market-desc" dir="auto">
        {item.description}
      </p>
      <p className="market-ready">
        {item.lessons_ready} of {item.lessons_total} lessons ready
      </p>
    </article>
  );
}

/** Browse, search, filter and sort the public catalogue (E15-07). */
export function MarketplacePage() {
  const [applied, setApplied] = useState(fromLocation);
  const [draft, setDraft] = useState(applied.q);
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<Result | null>(null);
  const [more, setMore] = useState<"idle" | "loading" | MarketFailure>("idle");
  const busy = useRef(false);
  const params = useMemo(() => applied, [applied]);
  const key = `${catalogueUrl(params)}#${attempt}`;

  useEffect(() => {
    let active = true;
    fetchCatalogue(params).then(
      (page) =>
        active && setResult({ key, items: page.items, next: page.next_cursor }),
      (error: unknown) =>
        active &&
        setResult({
          key,
          items: [],
          next: "",
          failure: marketFailure(error),
        }),
    );
    return () => {
      active = false;
    };
  }, [params, key]);

  const apply = (patch: Partial<typeof applied>) => {
    const next = { ...applied, ...patch };
    const query = new URLSearchParams();
    if (next.q) query.set("q", next.q);
    if (next.category) query.set("category", next.category);
    if (next.language) query.set("language", next.language);
    query.set("sort", next.sort);
    window.history.replaceState(null, "", `/marketplace?${query}`);
    setMore("idle");
    setApplied(next);
  };

  const loading = result?.key !== key;
  const loadMore = () => {
    if (!result?.next || busy.current) return;
    busy.current = true;
    setMore("loading");
    const at = key;
    fetchCatalogue({ ...params, cursor: result.next })
      .then(
        (page) => {
          setResult((r) => {
            if (!r || r.key !== at) return r;
            const seen = new Set(r.items.map((i) => i.course_id));
            return {
              ...r,
              items: [
                ...r.items,
                ...page.items.filter((i) => !seen.has(i.course_id)),
              ],
              next: page.next_cursor,
            };
          });
          setMore("idle");
        },
        (error: unknown) => setMore(marketFailure(error)),
      )
      .finally(() => {
        busy.current = false;
      });
  };

  return (
    <div className="market" aria-busy={loading}>
      <h1>Marketplace</h1>
      <form
        className="market-search"
        role="search"
        onSubmit={(e) => {
          e.preventDefault();
          apply({ q: draft.trim() });
        }}
      >
        <input
          type="search"
          aria-label="Search public courses"
          placeholder="Search public courses"
          maxLength={100}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
        <button type="submit" className="duo-button">
          Search
        </button>
      </form>
      <div className="market-controls">
        <div className="market-chips" role="group" aria-label="Category">
          {[{ id: "", label: "All" }, ...categories].map((c) => (
            <button
              key={c.id}
              type="button"
              className="market-chip"
              aria-pressed={applied.category === c.id}
              onClick={() => apply({ category: c.id })}
            >
              {c.label}
            </button>
          ))}
        </div>
        <label className="market-select">
          Course language
          <select
            value={applied.language}
            onChange={(e) => apply({ language: e.target.value })}
          >
            {languages.map((l) => (
              <option key={l.id} value={l.id}>
                {l.label}
              </option>
            ))}
          </select>
        </label>
        <label className="market-select">
          Sort
          <select
            value={applied.sort}
            onChange={(e) => apply({ sort: e.target.value as Sort })}
          >
            {sorts.map((s) => (
              <option key={s.id} value={s.id}>
                {s.label}
              </option>
            ))}
          </select>
        </label>
      </div>

      {loading && (
        <p role="status">
          <span className="sharing-spinner" aria-hidden="true" />
          Loading courses…
        </p>
      )}
      {!loading && result?.failure && (
        <div className="sharing-error" role="alert">
          <p>{marketMessages[result.failure]}</p>
          <button
            type="button"
            className="duo-button"
            onClick={() => setAttempt(attempt + 1)}
          >
            Retry
          </button>
        </div>
      )}
      {!loading && result && !result.failure && result.items.length === 0 && (
        <section className="market-empty" data-testid="market-empty">
          <h2 dir="auto">
            {applied.q
              ? `No courses match “${applied.q}”`
              : "No courses here yet"}
          </h2>
          <p>Try fewer words or another category, or make it yourself.</p>
          <a
            className="duo-button secondary"
            href={`/courses/create${applied.q ? `?topic=${encodeURIComponent(applied.q)}` : ""}`}
          >
            Create this course
          </a>
        </section>
      )}
      {!loading && result && !result.failure && result.items.length > 0 && (
        <>
          <p className="sharing-note" aria-live="polite">
            {result.items.length} course{result.items.length === 1 ? "" : "s"}
            {result.next ? " so far" : ""}
          </p>
          <div className="market-results" data-testid="market-results">
            {result.items.map((item) => (
              <Card key={item.course_id} item={item} />
            ))}
          </div>
          {typeof more === "string" &&
            more !== "idle" &&
            more !== "loading" && (
              <div className="sharing-error" role="alert">
                <p>{marketMessages[more]}</p>
              </div>
            )}
          {result.next && (
            <button
              type="button"
              className="duo-button secondary market-more"
              disabled={more === "loading"}
              onClick={loadMore}
            >
              Load more
            </button>
          )}
        </>
      )}
    </div>
  );
}
