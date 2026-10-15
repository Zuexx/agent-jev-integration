# 交接紀錄

給下一個接手的 session（人或 agent）。依日期往下加，修掉或作廢的項目直接刪。
這裡只放「現在」的狀態；決定和討論的完整經過記在 `docs/decisions.md`（只增不刪，可追溯）。

## 2026-10-14

### 已做的決定

- **不做「prompt 裡指定模型就覆蓋 Jev 判斷」**（PR #2 已關閉）。使用者要用特定模型時自己 `/model opus`。
  原因：要靠正則猜使用者意思，有漏判與誤判（「do it with opus」、「don't break anything and use opus」），
  而手動切換更穩、更可預期。不要再提同一個修法。
- **haiku 當主導 session 的適用範圍**：規格清楚、能用測試或明確條件驗收的工作（README 的實測都屬於這類）。
  開放式工作（影片、設計、研究、文件架構）和 MCP 多步驟操作（例如 ComfyUI），一開始就切 sonnet 或 opus。
  依據是下面的影片實驗。

### 影片實驗（opus 主導 vs haiku 主導）

同一份分鏡做了三種分工的 jevcc 介紹影片：

| 分工 | 結果 |
|---|---|
| haiku 自己寫分鏡、自己做 | 比喻式說明動畫，方向和參考影片（錄真實程式）不同 |
| jev-opus 寫動態規格、haiku 做 | 仍是同一套比喻動畫：opus 只拿到「替現有分鏡補規格」的題目，方向在它介入前已定 |
| opus 寫規格並審查成品、haiku 寫程式與錄影 | 兩輪後品質接近 opus 自己做；haiku 執行好，但自我檢查漏掉殘留面板、無字幕空檔等問題 |

結論：方向與審查要在強模型手上；haiku 做明確的執行沒問題。jevcc 目前逐句判斷，碰不到「方向」這種決定。

影片專案在 `../jevcc-intro-video/`（不在 git，只在原本那台 Windows 機器上）：`demo-opus/`、`demo-haiku-led/`，成品在 `out/`。

### 已知問題（未修）

- `jev-*` subagent 的 `tools` 寫死為 `Read, Grep, Glob, Bash, Edit, Write`（`internal/install/agents/`）：
  拿不到 MCP 工具，也不能用 skill（清單裡沒有 `Skill`）。升級去做 MCP 工作反而做不了。
  已查證（2026-10-15，官方 sub-agents 文件）：拿掉 `tools` 就繼承主對話全部工具，含 MCP 與 `Skill`；
  `mcpServers` 欄位對 plugin subagent 無效；`AskUserQuestion` 會被平台從所有 subagent 移除，
  「不能問使用者」只能靠 prompt 要它把問題寫進報告。拿掉 `tools` 時要一併改 `install.AgentsJSON()`，
  否則會送出 `"tools": [""]`。怎麼改還沒定，見 `docs/decisions.md` 的 D3。
- Stop review 只在改了程式碼時才跑（`not_code` 跳過）。影片、文件等非程式碼產出沒有人驗收。

### 改進點子（未做，待討論）

- 對話層級判斷：session 開頭（或每隔幾句）問 Jev「這是不是開放式、要邊做邊判斷的工作」，是就提示使用者切 `/model`，而不是派 subagent。
- 非程式碼回合的驗收：產出檔案（影片、文件）時也讓強模型看一下成品。
- 重新評測主導模型：同一批任務（程式題和開放式任務都要有）分別由 opus、sonnet、haiku 主導，搭配 Jev，
  比較成品品質和總成本。用結果決定哪類工作該由哪個模型主導，也檢查「往下派工」預設關閉是否合理
  （`+14%` 只剩 `hook.go` 的說明字串可查，而 bench 的任務都是可用測試檢查的題目）。下一步就做這個，方向與要求待和使用者討論。

### 工作習慣

- 回覆使用者用繁體中文。
- `gh pr merge` 會被 Claude Code 的 auto mode 擋下（Merge Without Review）；要使用者明確說要合併才做。
