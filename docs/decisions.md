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
- 使用者的要求（2026-10-15）：
  - **有所本、可重現**：其他使用者看到 artifact 裡的增益數字，不知道怎麼用、怎麼測，也無法確認自己會有一樣的節省。
    評測要附上足夠資訊，讓想驗證的人能照著做、自己核對。
  - opus、sonnet、haiku 三種主導都要測；往上派工、往下派工都要能做。
  - 題目要夠多元：除了程式題，也要有 MCP 操作（ComfyUI）、導演或設計類、規劃類。凡是 opus 能主導的工作，都要拿其他模型主導來比。
  - 工具限制（現狀）和工具全開兩種情境的成本都要列出比較（對應 D3）。
- 結果出來後回頭更新 D2、D3。

## D5 — MCP 操作類題目用 Blender，不用 ComfyUI 或 Godot

- 日期：2026-10-15　狀態：**已決定**
- 內容：評測中「用 MCP 操作創作工具」這類題目改用 Blender 4.5 LTS（headless `blender -b --python`），MCP 由本專案自己寫的薄 server 提供。
- 理由：開發機（Intel Mac、無獨立 GPU）跑不了 ComfyUI；Blender 免費跨平台，別人容易重現；成品可用 bpy 確定性檢查。
  Godot 的主要工作是寫 GDScript，接近程式題，量不到「操作工具做出視覺成品」。Intel Mac 只能用到 4.5 LTS（官方：5.0 起只支援 Apple Silicon）。
- 出處：使用者同意以 brew 安裝（`/Applications/Blender.app`，4.5.14 LTS）；評測計畫 `docs/eval-plan.md` 3.4。

## D6 — 評測一律在隔離環境執行；舊的 bench 數字不可當作公開依據

- 日期：2026-10-15　狀態：**已決定**
- 內容：每次評測執行用暫存的 `CLAUDE_CONFIG_DIR` 與 `JEVCC_HOME`，並檢查 init 事件確認沒有載入清單以外的東西。
- 理由：舊 bench 沿用作者的 `~/.claude`；實測一句 `reply ok` 就載入 3 個 SessionStart hook、7 個 plugin、5 個 MCP server、79 個 skill。
  另外舊的 10 題 opus 與 haiku 都 100% 通過，README 的「省 97%」量自沒有鑑別力的題目。
- 後續：新評測完成後更新 README 的數字與說明（待使用者確認改法）。
- 出處：`docs/eval-plan.md` 第 0 節。

## D7 — 評測實作細節（推理強度、評審、一致性指標）

- 日期：2026-10-15　狀態：**已決定**（正式執行前）
- 內容：每次執行固定 `--effort medium`，評審用 opus＋`high`；評審為盲評兩兩比較、交換順序兩次，結論不一致記平手；
  人工與評審的一致性改用「人工分數轉偏好後的 Cohen's kappa」。
- 理由：medium 是 Claude Code 一般預設，最接近使用者實際會得到的；評審需要較高推理強度但只占少量用量；
  評審是兩兩比較、人工是絕對分數，原計畫的加權 kappa 無法直接套用。
- 出處：`docs/eval-plan.md` 變更紀錄（2026-10-15 實作期間的調整）。
