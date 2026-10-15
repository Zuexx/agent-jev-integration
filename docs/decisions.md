# 決策紀錄

決定與討論的經過，**只增不刪**：改變心意時新增一條並在舊條目標「被 Dn 取代」，不要改寫舊內容。
`docs/handoff.md` 只放現在的狀態；這裡放「為什麼」。

每條：編號、日期、狀態（已決定／保留待評測／已取代）、內容、理由或依據、出處（PR、commit、文件）。

---

## D1 — 不做「prompt 裡指定模型就覆蓋 Jev 判斷」

- 日期：2026-10-14　狀態：**已決定**
- 內容：使用者要特定模型時自己 `/model`，jevcc 不從 prompt 文字猜。
- 理由：要靠正則猜意思，漏判與誤判都有（「do it with opus」、「don't break anything and use opus」）；手動切換更穩、可預期。
- 出處：PR #2（已關閉）、commit f814c2d。

## D2 — haiku 當主導 session 的適用範圍

- 日期：2026-10-14　狀態：**已決定**（暫定，D4 的評測可能修改）
- 內容：haiku 主導只用於規格清楚、能用測試或明確條件驗收的工作；開放式工作（影片、設計、研究、文件架構）和 MCP 多步驟操作一開始就用 sonnet 或 opus。
- 依據：jevcc 介紹影片實驗（三種分工，見 `docs/handoff.md`）。opus 寫規格並審查、haiku 執行的組合兩輪後接近 opus 自己做；haiku 主導時方向在 opus 介入前就定了。
- 出處：commit f814c2d。

## D3 — `jev-*` subagent 的工具限制

- 日期：2026-10-15　狀態：**保留待評測**（等 D4）
- 問題：`tools` 寫死為 `Read, Grep, Glob, Bash, Edit, Write`，拿不到 MCP 工具，也不能用 skill。
- 查證（官方 sub-agents 文件，2026-10-15）：
  - 省略 `tools` 會繼承主對話全部工具，含 MCP 與 `Skill`；可用 `disallowedTools` 排除，`mcp__<server>__*` 可當萬用字元。
  - `mcpServers` 欄位對 plugin subagent 無效。
  - `AskUserQuestion`、`EnterPlanMode` 會從所有 subagent 移除，寫進 `tools` 也一樣；只能靠 prompt 要它把要問的寫進報告。
  - 背景 subagent（預設）的內建工具較少，但 MCP 工具和 `Skill` 都保留。
  - 省略 `tools` 時 `install.AgentsJSON()` 會送出 `[""]`，要一併改。
- 提案（未採用）：`jev-haiku`、`jev-sonnet`、`jev-opus` 拿掉 `tools`；`jev-review` 不動；四個 agent 的 prompt 加「需要使用者決定時停下來寫進報告」。
- 保留的原因：使用者認為工具取捨取決於「誰主導、誰執行」的分工，要先有 D4 的評測結果再定。

## D4 — 重新評測主導模型（opus / sonnet / haiku）與 Jev 的成本差異

- 日期：2026-10-15　狀態：**進行中**（方向與要求待討論）
- 動機：介紹影片的實際案例中，haiku 主導與 opus 主導的品質落差很大，懷疑 haiku 主導在開放式工作上整體比不上 opus 主導。
- 討論中的分析（未定案，僅供評測設計參考）：
  - 落差主要來自主導模型的判斷（方向、拆解、驗收），不是工具。Remotion 只需要 Bash，現有工具就夠，影片並未被 `tools` 卡住。
  - 「便宜模型主導、難題才升級」的上限在於：要不要升級、升級時問什麼，都由較弱的模型決定；程式題有測試把關，開放式工作沒有。
  - 可能的分工：規格清楚的工作由 haiku/sonnet 主導並升級；開放式工作由 opus 主導，把明確的執行交給 haiku/sonnet，再由 opus 驗收。
  - 「往下派工」預設關閉的依據 `+14%` 只剩 `internal/hook/hook.go` 的說明字串可查，而 `jevcc bench` 的任務都是用測試或檔案內容檢查的題目，沒有涵蓋開放式工作。
- 結果出來後回頭更新 D2、D3。
