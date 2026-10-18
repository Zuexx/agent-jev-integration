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
- Stop review 的觸發條件是「這一回合用了 Write/Edit/MultiEdit/NotebookEdit」（`verify.Turn.EditedCode`，名稱有誤導），
  不看檔案類型：markdown 計畫、Remotion 的 .tsx 也會被審查（試跑中 jev-review 曾以 6 次 Edit 重寫 haiku 的計畫）。
  反過來，只經由 Bash 或 MCP 產出的成品（Blender 場景與渲染、指令產生的檔案）不會被審查。
  2026-10-15 更正；評測期間不改這個行為，以免評測對象變動。

### 改進點子（未做，待討論）

- 對話層級判斷：session 開頭（或每隔幾句）問 Jev「這是不是開放式、要邊做邊判斷的工作」，是就提示使用者切 `/model`，而不是派 subagent。
- 非程式碼回合的驗收：產出檔案（影片、文件）時也讓強模型看一下成品。
- 重新評測主導模型：同一批任務（程式題和開放式任務都要有）分別由 opus、sonnet、haiku 主導，搭配 Jev，
  比較成品品質和總成本。用結果決定哪類工作該由哪個模型主導，也檢查「往下派工」預設關閉是否合理
  （`+14%` 只剩 `hook.go` 的說明字串可查，而 bench 的任務都是可用測試檢查的題目）。下一步就做這個，方向與要求待和使用者討論。

### 工作習慣

- 回覆使用者用繁體中文。
- `gh pr merge` 會被 Claude Code 的 auto mode 擋下（Merge Without Review）；要使用者明確說要合併才做。

## 2026-10-15

### 進度：重新評測（D4，計畫 `docs/eval-plan.md` 已登記）

- 實作在 worktree `../agent-jev-integration-eval`，分支 `feat/eval-isolation`（PR 待開）。重跑步驟見 `docs/eval-howto.md`。
- 已完成：隔離執行（暫存 HOME／CLAUDE_CONFIG_DIR／JEVCC_HOME，init 事件檢查）、`bench/open` 13 題（含 Blender、Remotion）、
  `--validate`、Blender MCP（`jevcc blender-mcp`）、組別 `<主導>/<off|up|down|both>[/open]`、額度控管與 `--resume`、
  盲評 `jevcc judge`、人工評分頁 `jevcc rate`、報告 `jevcc report`。
- 下一步：端到端試跑確認整條流程 → 依計畫 9.3 跑 P1（主導模型基準線）。
- 試跑觀察：開放式題目的確定性檢查 haiku 也幾乎全過，但成品品質差距明顯（blender-hero）；比較要靠評審與人工評分。

### 這台機器上評測用到、結束後要清的東西

- `~/.jevcc-bench/`：開始前的設定快照（`baseline-20261015.snap`）、claude-2 的 token、`save-token.sh`、Remotion 快取。
  結束時：`bench/envsnap.sh snap` 再拍一次並 diff，刪掉這個目錄，到帳號設定撤銷 token。
- Blender 4.5.14 LTS 以 brew 安裝於 `/Applications/Blender.app`（使用者同意）；是否移除由使用者決定。
- worktree `../agent-jev-integration-eval` 在 PR 合併後移除。

### 帳號

- 兩個 Pro 帳號：main（`~/.claude`）與 claude-2（`~/.claude-2`，每週三 20:00 重置）。評測主要用 claude-2，每週保留 30%。
- shell 的 `claude` 是 cswitch 的包裝函式；要跑 Claude Code 本身的子指令（如 `setup-token`）用 `command claude`。

## 2026-10-18

### 進度：評測 P1、P2 完成（D4），依結果調整（D9）

- 報告：`docs/eval-report.md`。P2 第一次作廢（螢幕鎖定後 Jev 呼叫全失敗），已重跑。人工評分只評視覺類，兩輪（使用者、第三方）。
- 採用 D9（1、2、3）。D10：P3、P4 不跑；subagent 用 MCP（`/jev mcp`）與 guard（`/jev guard`）改成自選、預設關。
- D11：往下委派預設開，只限程式工作（Jev 多答 `CodeQuestion`，p ≥ 0.8）。
- 評測資料在 `~/.jevcc-bench/results/`（p1、p2b、p12b＝合併檔與評審、p12/rate-round1＝第 1 輪人工、p12/rate.html＝第 2 輪）。
- 背景跑評測時螢幕不可鎖定、不可闔蓋（D9 第 3 條修好前的教訓；修好後仍建議如此）。

### 發布與收尾（2026-10-18）

- 發布：`v0.1.0`（GoReleaser，六個平台；`jevcc version`）。評測原始資料：release `eval-2026-10`（約 395 MB，含 `results/README.md`）。
  repo 目前是 private，所以 README 的安裝方式以 `gh release download` 為主；要公開由使用者決定。
- 收尾已做：環境快照比對（評測沒有改到使用者設定；`~/.claude-2` 的變化來自 cswitch 在 10/18 06:47 把 `~/.claude/projects`
  複製過去、agents 改成連結）、刪除 `~/.jevcc-bench`、移除 eval worktree、`brew uninstall --cask blender`
  （Homebrew 順帶自動移除了沒人用的 `cabal-install`、`pycparser`）。
- 待使用者做：到 claude.ai 帳號設定撤銷 claude-2 的 setup-token（評測用，檔案已隨 `~/.jevcc-bench` 刪除，但 token 本身仍有效）。
- 本機的 `~/go/bin/jevcc` 仍是舊版：用 release 或 `go install ./cmd/jevcc` 更新。更新後 guard 變成預設關（`/jev guard on` 可開）、
  往下委派變成預設開但只限程式工作（`"delegate": false` 可關）。
- README 建議設定改為不設預設模型（不再建議 `--default-model haiku`），依 D9／D11：主導維持 opus／sonnet，程式工作靠委派省錢。
