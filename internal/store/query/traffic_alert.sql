-- name: GetTrafficAlertState :one
SELECT alerted_at FROM traffic_alert_state WHERE service_id = ? AND threshold = ?;

-- name: UpsertTrafficAlertState :exec
INSERT INTO traffic_alert_state (service_id, threshold, alerted_at)
VALUES (?, ?, ?)
ON CONFLICT (service_id, threshold) DO UPDATE SET alerted_at = excluded.alerted_at;
