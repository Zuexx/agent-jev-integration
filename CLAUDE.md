# jevcc

先讀 `docs/handoff.md`：上一個 session 留下的決定、已知問題和待討論的點子。設計與限制見 `docs/design.md`，用法見 `README.md`。

- 回覆使用者用繁體中文。
- 開發檢查：`gofmt -l . && go vet ./... && go test ./...`
- 修改 `internal/install/commands/*.md` 後執行 `go run ./cmd/jevcc plugin-files plugin` 同步 `plugin/`。
- 交接內容放在 `docs/handoff.md` 並進 git，換機器時 `git pull` 就拿得到；不要只寫在本機的記憶目錄。
