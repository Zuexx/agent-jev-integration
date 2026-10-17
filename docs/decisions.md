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

- 日期：2026-10-14　狀態：**已決定**（2026-10-18 依 D4 評測確認並加強，見 D9）
- 內容：haiku 主導只用於規格清楚、能用測試或明確條件驗收的工作；開放式工作（影片、設計、研究、文件架構）和 MCP 多步驟操作一開始就用 sonnet 或 opus。
- 依據：jevcc 介紹影片實驗（三種分工，見 `docs/handoff.md`）。opus 寫規格並審查、haiku 執行的組合兩輪後接近 opus 自己做；haiku 主導時方向在 opus 介入前就定了。
- 出處：commit f814c2d。

## D3 — `jev-*` subagent 的工具限制

- 日期：2026-10-15　狀態：**改為使用者自選**（2026-10-18，見 D10）
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

## D8 — 本週不保留 claude-2 的 30% 週額度（2026-10-17）

- 使用者決定：claude-2 這週（到 2026-10-21 20:00 重置）不保留 30%，評測與評審可用到 97%（留一點誤差）。主帳號照舊不碰。
- 依 eval-plan 9.3 的順序跑剩下的：探索性比較評審（haiku＋往上 vs haiku 主導）→ P3（sonnet 往上／往下／雙向 × 工具限制／全開，
  7 題需要 MCP 或 skill 的題目）→ P3 評審（全開 vs 限制）→ P4 易題（每組 1 次）→ P4 sonnet 往上／往下／雙向的其他非易題。
  P4 的評審留到下週。估計用到 85–90%（照 P1、P2 的實際花費推算，可能有 ±30% 誤差）。
- 為什麼：週三前能跑完 P3（回答 Q4），這週額度用不完也不會留到下週。

## D9 — 依評測結果（P1、P2 重跑）調整 jevcc（2026-10-18）

- 使用者採用 `docs/eval-report.md` 提案的 1、2、3：
  1. **D2 加強**：haiku 主導只用於規格清楚、能用測試驗收的程式工作；開放式與視覺類一開始就用 opus（或 sonnet）主導，也不要用
     「haiku＋往上升級」替代（花費約 haiku 的 11 倍、opus 的一半，品質大多仍輸 opus 主導）。README 的建議設定加上說明。
  2. **往下委派維持選用**：opus 主導開委派品質不變，只有程式類明確省錢（約 −16%），整體 −6% 無明確差異；README 寫清楚。
  3. **修 Jev 呼叫的 TLS 失敗**：macOS 螢幕鎖定後 Go 的系統憑證驗證失敗（`x509: OSStatus -26276`），hook 靜默失效（P2 第一次
     因此作廢）。只在這個錯誤時改用 `/etc/ssl/cert.pem` 再試一次；狀態列的錯誤標示由「timeouts」改為「Jev errors」。
- 依據：`docs/eval-report.md`；P2 第一次作廢的經過見 `docs/eval-plan.md` 變更紀錄。
- 驗證（計畫第 12 節）：第 3 條以單元測試（模擬系統驗證失敗、改用 PEM bundle）與實機確認（`/etc/ssl/cert.pem` 可驗證
  api.typesafe.ai）；鎖定螢幕下的實測待下次背景執行。第 1、2 條只改文件與建議，不改行為，沒有可在新題目上驗證的程式變更。
- D3 維持待評測（P3 未跑）。

## D10 — 不跑 P3、P4；subagent 用 MCP 與 guard 改成使用者自選、預設關（2026-10-18）

- 使用者決定：P3（subagent 工具全開 vs 限制）與 P4（補充）不跑。D3 不靠評測定案，改成開關讓使用者自己選。
- `agent_mcp`（`/jev mcp on|off`、`jevcc install --agent-mcp`）：開啟時 jev-haiku／jev-sonnet／jev-opus 的檔案拿掉 `tools`，
  繼承主對話全部工具（含 MCP 與 Skill）；jev-review 不變。預設關（即原本的限制）。只改寫 jevcc 安裝在使用者範圍的 agent
  檔，新對話才生效；plugin 安裝的 agent 是固定檔，維持限制。
- `guard`（`/jev guard on|off`、`jevcc install --guard`）：預設由開改為關。
- 已知取捨：
  - guard 改預設關後，沒有在設定裡寫 `"guard": true` 的既有使用者會失去危險動作確認（原本預設開）；它只會要求確認、不會擋，
    關掉後 Claude Code 自己的權限提示仍在。換來每次 Bash／寫檔少一次 Jev 呼叫（延遲與花費）。
  - agent_mcp 開啟後 subagent 能用 MCP 與 skill（冒煙測試中受限的 subagent 改用命令列 Blender 或 Bash 讀 skill 繞過），
    但也能用主對話所有 MCP 的寫入類工具；`AskUserQuestion` 等仍會被 Claude Code 從 subagent 移除。
- D3 狀態：由這條取代（不再待評測）。
