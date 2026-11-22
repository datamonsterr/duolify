# Design
Native column comparison with `$n::uuid` is the house style (chat, accounts, player, narration). A Go string that is not a canonical UUID makes Postgres fail with 22P02, so every handler that takes an id calls the shared canonical-UUID check first and answers with a client error. Where a value can legitimately be empty (an optional reservation or quote id), the statement uses `NULLIF($n::text,'')::uuid` so an empty value matches nothing instead of failing.

The event stream reads state quickly right after it opens or the state moves, then backs off, because lesson generation is one blocking provider call lasting tens of seconds. The first read happens before the stream headers so an unknown generation is a plain 404.

The index test records the statements a request actually ran (pgx tracer) and EXPLAINs them with their bound arguments, so it tests the shipped SQL rather than a copy. The planner is not constrained; tables are ANALYZEd after seeding.
