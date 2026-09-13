-- name: UpsertDailyUsage :exec
INSERT INTO daily_usage (service_id, day, in_bytes, out_bytes, fetched_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (service_id, day) DO UPDATE SET
  in_bytes   = excluded.in_bytes,
  out_bytes  = excluded.out_bytes,
  fetched_at = excluded.fetched_at;

-- name: ListDailyUsage :many
SELECT service_id, day, in_bytes, out_bytes, fetched_at
FROM daily_usage
WHERE service_id = sqlc.arg('service_id')
  AND day >= sqlc.arg('from')
  AND day <= sqlc.arg('to')
ORDER BY day DESC;

-- name: DeleteDailyUsageBefore :exec
DELETE FROM daily_usage WHERE day < ?;
