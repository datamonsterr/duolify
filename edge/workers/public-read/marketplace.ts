import { Client } from "pg";
import type { MarketplaceSurface } from "./policy";

export interface MarketplaceConfig {
  MARKETPLACE_PAGE_SIZE?: string;
  MARKETPLACE_MAX_PAGE_SIZE?: string;
  MARKETPLACE_MAX_QUERY_LENGTH?: string;
  MARKETPLACE_RATING_MIN_COUNT?: string;
  MARKETPLACE_RATING_PRIOR?: string;
}

interface MarketResult {
  body: object;
  status: number;
}

const categories = new Set([
  "science",
  "tech",
  "languages",
  "life_skills",
  "arts",
]);
const uuid = /^[a-f\d]{8}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{12}$/i;

function integerSetting(
  raw: string | undefined,
  fallback: number,
  max: number,
) {
  if (!raw || !/^\+?\d+$/.test(raw)) return fallback;
  const value = Number(raw);
  return value >= 1 && value <= max ? value : fallback;
}

function trimSpace(value: string) {
  return value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, "");
}

function refusal(error: string, status: number): MarketResult {
  return { body: { error }, status };
}

interface Position {
  k: number;
  t: number;
  a: number;
  i: string;
}

function decodeCursor(encoded: string, sort: string): Position | undefined {
  if (encoded.length > 512 || !/^[\w-]+$/.test(encoded)) return;
  try {
    const raw = atob(encoded.replaceAll("-", "+").replaceAll("_", "/"));
    const wire = JSON.parse(raw);
    if (!wire || typeof wire !== "object" || wire.s !== sort) return;
    if (
      Object.keys(wire).some((key) => !["s", "k", "t", "a", "i"].includes(key))
    )
      return;
    const { k = 0, t = 0, a = 0, i } = wire;
    if (
      !Number.isFinite(k) ||
      !Number.isSafeInteger(t) ||
      !Number.isSafeInteger(a) ||
      t < 0 ||
      a < 0 ||
      typeof i !== "string" ||
      !uuid.test(i)
    )
      return;
    return { k, t, a, i };
  } catch {
    return;
  }
}

function encodeCursor(sort: string, position: Position) {
  return btoa(JSON.stringify({ s: sort, ...position }))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replace(/=+$/, "");
}

// PostgreSQL formats the timestamp before node-postgres can round it to milliseconds.
const published = `rtrim(rtrim(to_char(published_at AT TIME ZONE 'UTC',
  'YYYY-MM-DD"T"HH24:MI:SS.US'), '0'), '.') || 'Z'`;
const common = `course_id, title, category, language, version, ${published} AS published_at,
  creator, lessons_ready, lessons_total, enrollment_count::integer,
  rating_sum::float8 / NULLIF(rating_count, 0) AS rating_average, rating_count`;

const courseSQL = `SELECT ${common}, description, tags, outline, sample_slides,
  '{"owner": false, "enrolled": false}'::jsonb AS viewer
  FROM edge_public.marketplace_course_v1 WHERE course_id = $1::uuid`;

const browseSQL = `WITH ranked AS (
  SELECT p.*,
    CASE $1::text WHEN 'popular' THEN popular_score WHEN 'rating' THEN
      COALESCE(($6::int * COALESCE(m.mean, $5::float8) + rating_sum::float8) /
        NULLIF($6::int::float8 + rating_count::float8, 0), m.mean, $5::float8)
      ELSE 0::float8 END AS sort_key,
    CASE $1::text WHEN 'popular' THEN enrollments_30d::bigint WHEN 'rating' THEN rating_count::bigint
      ELSE 0::bigint END AS sort_tie
  FROM edge_public.marketplace_browse_v1 p CROSS JOIN edge_public.marketplace_rating_v1 m
  WHERE ($2::text = '' OR category = $2)
    AND ($3::text = '' OR language = $3)
    AND ($4::text = '' OR search_document @@ websearch_to_tsquery('simple', $4))
), page AS (
  SELECT * FROM ranked
  WHERE $7::uuid IS NULL OR (sort_key, sort_tie, published_at, course_id) <
    ($8::float8, $9::bigint, timestamptz 'epoch' + $10::bigint * interval '1 microsecond', $7::uuid)
  ORDER BY sort_key DESC, sort_tie DESC, published_at DESC, course_id DESC
  LIMIT $11
)
SELECT row_to_json(item) AS item, page.sort_key, page.sort_tie,
       (extract(epoch FROM page.published_at) * 1000000)::bigint AS published_microseconds
FROM page
CROSS JOIN LATERAL (SELECT ${common},
  CASE WHEN char_length(description) > 280 THEN left(description, 280) || '…' ELSE description END AS description
) item
ORDER BY page.sort_key DESC, page.sort_tie DESC, page.published_at DESC, page.course_id DESC`;

interface BrowseRow {
  item: { course_id: string };
  sort_key: number;
  sort_tie: string;
  published_microseconds: string;
}

function browseParameters(url: URL, config: MarketplaceConfig) {
  const values = url.searchParams;
  const q = trimSpace(values.get("q") ?? "");
  const category = values.get("category") ?? "";
  const language = trimSpace(values.get("language") ?? "").toLowerCase();
  const sort = values.get("sort") || "popular";
  const rawLimit = values.get("limit") ?? "";
  const maxQuery = integerSetting(
    config.MARKETPLACE_MAX_QUERY_LENGTH,
    100,
    200,
  );
  if (
    [...q].length > maxQuery ||
    /\p{Cc}/u.test(q) ||
    (category && !categories.has(category)) ||
    (language && !/^[a-z]{2,3}$/.test(language)) ||
    !["new", "rating", "popular"].includes(sort) ||
    (rawLimit && !/^[+-]?\d+$/.test(rawLimit))
  )
    return refusal("invalid_query", 400);
  const requested = BigInt(rawLimit || "0");
  if (requested < -(2n ** 63n) || requested >= 2n ** 63n)
    return refusal("invalid_query", 400);

  const limit = Math.min(
    requested > 0
      ? Number(requested)
      : integerSetting(config.MARKETPLACE_PAGE_SIZE, 20, 100),
    integerSetting(config.MARKETPLACE_MAX_PAGE_SIZE, 50, 100),
  );
  const encoded = values.get("cursor");
  const cursor = encoded ? decodeCursor(encoded, sort) : undefined;
  if (encoded && !cursor) return refusal("invalid_cursor", 400);

  const configuredPrior = Number(config.MARKETPLACE_RATING_PRIOR);
  const prior =
    configuredPrior >= 1 && configuredPrior <= 5 ? configuredPrior : 3.5;
  const minimum = integerSetting(
    config.MARKETPLACE_RATING_MIN_COUNT,
    10,
    10000,
  );
  return {
    sort,
    limit,
    args: [
      sort,
      category,
      language,
      q,
      prior,
      minimum,
      cursor?.i ?? null,
      cursor?.k ?? 0,
      cursor?.t ?? 0,
      cursor?.a ?? 0,
      limit + 1,
    ],
  };
}

export async function readMarketplace(
  binding: Hyperdrive,
  request: Request,
  surface: MarketplaceSurface,
  config: MarketplaceConfig,
  context: ExecutionContext,
): Promise<MarketResult | undefined> {
  const url = new URL(request.url);
  const id = url.pathname.split("/").pop();
  // Go accepts historical non-canonical UUID forms and handles ambiguous query encodings.
  if (surface === "course_page" && (!id || !uuid.test(id))) return;
  const browse =
    surface === "marketplace" ? browseParameters(url, config) : undefined;
  if (browse && "status" in browse) return browse;
  const client = new Client({
    connectionString: binding.connectionString,
    connectionTimeoutMillis: 2000,
    query_timeout: 2500,
  });
  try {
    await client.connect();
    if (!browse) {
      const { rows } = await client.query(courseSQL, [id]);
      return rows.length
        ? { body: rows[0], status: 200 }
        : refusal("course_unavailable", 404);
    }

    const { rows } = await client.query<BrowseRow>(browseSQL, browse.args);
    const selected = rows.slice(0, browse.limit);
    const last = selected.at(-1);
    const next =
      rows.length > browse.limit && last
        ? encodeCursor(browse.sort, {
            k: last.sort_key,
            t: Number(last.sort_tie),
            a: Number(last.published_microseconds),
            i: last.item.course_id,
          })
        : "";
    return {
      body: { items: selected.map((row) => row.item), next_cursor: next },
      status: 200,
    };
  } finally {
    // pg-cloudflare 1.4.1 emits an asynchronous error when closing a healthy socket.
    // Connect and query failures above still reject and fall back to Go.
    client.on("error", () => {});
    context.waitUntil(client.end().catch(() => {}));
  }
}
