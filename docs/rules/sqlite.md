# SQLite / sqlc / modernc 驱动

## sqlc 对重复的 `?` 占位符会生成 `Day` / `Day_2` 这种名字

**现象**：`WHERE service_id = ? AND day >= ? AND day <= ?` 生成的结构体字段是 `ServiceID`、`Day`、`Day_2`，调用处完全看不出 `Day_2` 是区间的哪一端。

**原因**：sqlc 对匿名占位符按首次出现的列名命名，重名就加数字后缀。

**解决**：用 `sqlc.arg('from')` / `sqlc.arg('to')` 显式命名：

    WHERE service_id = sqlc.arg('service_id')
      AND day >= sqlc.arg('from')
      AND day <= sqlc.arg('to')

生成 `ServiceID`、`From`、`To`。

**相关文件**：`internal/store/query/daily_usage.sql`

## modernc 驱动的连接串用 `_pragma=` 传 PRAGMA

**现象**：直接开库后并发写入偶发 `database is locked`；外键约束默认不生效。

**原因**：SQLite 默认 journal 模式是 delete，且 modernc 驱动需要通过 DSN 指定 PRAGMA。

**解决**：DSN 写成

    file:<path>?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)

并配 `db.SetMaxOpenConns(1)`。驱动名是 `sqlite`。

**相关文件**：`internal/store/store.go`

## sqlc 生成的代码不能手改

**现象**：直接修改 `internal/store/sqlcgen/*.go` 后，下次 `sqlc generate` 会被覆盖。

**原因**：该目录是生成产物。

**解决**：改 `internal/store/schema.sql` 或 `internal/store/query/*.sql` 后重新 `sqlc generate`，用 `git diff --exit-code internal/store/sqlcgen` 在 CI 里防止忘记生成。

**相关文件**：`sqlc.yaml`、`Makefile`
