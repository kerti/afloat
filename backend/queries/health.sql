-- Readiness probe for GET /health. A round-trip through the pool proves more
-- than pgx's own Ping: it takes a connection from the pool, executes, and scans,
-- which is the path every real query takes. A pool that is exhausted or pointed
-- at a database the role cannot read fails here and passes a bare Ping.

-- name: Ping :one
SELECT 1::int AS ok;
