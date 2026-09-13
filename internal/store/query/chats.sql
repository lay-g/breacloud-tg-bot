-- name: ListChatIDs :many
SELECT chat_id FROM chats ORDER BY chat_id;

-- name: GetChat :one
SELECT chat_id, greeted_at, created_at FROM chats WHERE chat_id = ?;

-- name: GetChatGreetedAt :one
SELECT greeted_at FROM chats WHERE chat_id = ?;

-- name: InsertChat :exec
INSERT OR IGNORE INTO chats (chat_id, created_at) VALUES (?, ?);

-- name: DeleteChat :exec
DELETE FROM chats WHERE chat_id = ?;

-- name: MarkChatGreeted :exec
UPDATE chats SET greeted_at = ? WHERE chat_id = ?;
