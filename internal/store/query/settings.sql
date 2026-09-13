-- name: GetSettings :one
SELECT report_enabled, report_time, traffic_alert_enabled, traffic_thresholds,
       expiry_alert_enabled, expiry_days, updated_at
FROM settings
WHERE id = 1;

-- name: InsertDefaultSettings :exec
INSERT OR IGNORE INTO settings (id, updated_at) VALUES (1, ?);

-- name: UpdateSettings :exec
UPDATE settings SET
  report_enabled        = ?,
  report_time           = ?,
  traffic_alert_enabled = ?,
  traffic_thresholds    = ?,
  expiry_alert_enabled  = ?,
  expiry_days           = ?,
  updated_at            = ?
WHERE id = 1;
