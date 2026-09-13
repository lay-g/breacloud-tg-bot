-- name: GetJobRun :one
SELECT ran_at FROM daily_job_runs WHERE day = ? AND job = ?;

-- name: InsertJobRun :exec
INSERT OR IGNORE INTO daily_job_runs (day, job, ran_at) VALUES (?, ?, ?);

-- name: DeleteJobRunsBefore :exec
DELETE FROM daily_job_runs WHERE day < ?;
