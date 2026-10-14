# jevcc — Jev × Claude Code

把 TypeSafe 的 **Jev**（System 1 決策模型：只回答選擇題／打分／是非題，回傳機率，輸入 $0.042 / 百萬 token、輸出免費）
接到 Claude Code 裡「本質是在做判斷」的位置，生成與推理仍交給 Claude（System 2）。

依影片（`docs/transcript.zh-TW.txt`）提出的三個位置依序實作，設計與限制見 `docs/design.md`。

| 位置 | 功能 | 入口 |
|---|---|---|
| 1 幫 Claude 省錢 | 每句 prompt 先由 Jev 判斷：該用哪個等級的模型、該載哪個 skill、是否是「大量同題判斷」 | `/jev on\|off\|status\|stats`（hook 本地處理，不呼叫模型）、`jevcc ask` |
| 2 海量分類／打分 | Jev 粗篩全部資料、Claude 只看低信心與高分的列 | `/jev-classify`、`jevcc classify` |
| 3 寫完後的驗證 | 語意 Linter（白話規則）、百問清單 Code Review | `/jev-lint`、`/jev-review`、`jevcc lint`、`jevcc review` |
| 收尾 | 翻你過去的對話，找出其實只是分類／篩選、可交給 Jev 的步驟 | `/jev-audit` |

> 沒做：影片中「數十個 headless 瀏覽器對抗測試」需要 Browser-use 的 Jev 版瀏覽器 agent，不在本專案範圍。

## 先講清楚：「Jev 幫你挑模型」實際上怎麼運作

官方文件明講 Jev **不能**當 Claude Code 的主模型，hook 也**無法**切換當前 session 的模型。所以：

- **互動模式（`/jev on`）**：hook 把判斷結果以 `<jev_routing>` 區塊附加在 prompt 上：skill 建議、大量判斷時改用 `jevcc classify` 的提示。
  「委派給便宜模型的 subagent」建議**預設關閉**（`"delegate": true` 才開）：A/B 實測 Claude 照做後，短任務成本反而 +12%、時間 2 倍，
  因為主模型載入 Claude Code 系統提示的固定成本仍在。真正的換模型省錢走下一項。
- **Headless（`jevcc ask "<prompt>"`）**：先由 Jev 決定，再以 `claude --model <tier>` 啟動，這才是真正的逐句切換模型。
- **挑 skill**：照官方 Skill suggestion cookbook 的兩段式（第一次排名全部 skill + 「需不需要 skill」閘門，第二次精讀前三名可全部否決）。
  它降低「載錯 skill」，**不會**減少 Claude Code 本身送出的 skill 清單 token。
- 信心不足、逾時、出錯 → 不加任何內容，prompt 照常送出（hook 永不讓 claude 壞掉）。只有可執行的建議才會加進 context。

## 建議設定：從 claude 進、預設 haiku、讓 Jev 決定何時升級

預設值就是實測後的建議值，不用編輯 config。一行安裝：

```bash
jevcc install --default-model haiku --on-by-default
```

- `--default-model haiku`：Claude Code 預設開 haiku（原本的值會記下，`jevcc uninstall` 時還原）
- `--on-by-default`：每個新對話自動開啟 Jev（單一對話可用 `/jev off` 關掉）
- 主對話用什麼模型由 hook 自動偵測（對話紀錄裡最近一次回覆的模型 → Claude Code 設定），用 `/model` 中途切換也會跟著變

預設行為：往上升級開、往下委派關（實測反而貴）、事後驗收只在主對話比 opus 便宜時作用、危險動作檢查開（只會要求確認）。要改才需要 `~/.jevcc/config.json`，欄位見上方設定說明。

每句話的處理方式：

| 情況 | 會發生什麼 | 實測 |
|---|---|---|
| Jev 判定需要 opus（規劃、架構、不明原因的 bug、安全審查） | haiku 先把工作交給 `jev-opus` subagent | 17/17 照做；opus 題中 Jev 判對 17/21 |
| 一般工作 | haiku 自己做 | 10 個任務成本中位數 −96%，全數通過；5 道規格陷阱題 haiku 也全過 |
| haiku 說做完了 | Jev 驗收，有改程式卻沒跑過測試、有沒解決的錯誤等狀況就交給 opus 複查（`"verify": true`） | 簡單任務誤升級 0/2 |
| 要執行指令或寫檔 | 危險動作改成請你確認（`"guard": true`） | 危險抓到 21/22，安全誤擋 0/23（字串規則：12/22、2/23） |
| 問題跟專案程式碼有關 | 附上最可能相關的 3 個檔案 | 前三名挑對 19/20；haiku 讀的 token −20% |
| 你說出長期偏好或規則 | 提醒 Claude 存進記憶 | 20/20 抓到、一次性指示誤判 0/20 |

Jev 比較會判錯的題型：「找資料寫研究」和「整理領域用語」。遇到時自己 `/model opus`。

驗證指令：`jevcc escalateval`、`jevcc guardeval`、`jevcc findeval`、`jevcc memeval`、`jevcc bench --tasks bench/hard|bench/nav`。

## statusline 與防護

安裝時會把現有的 statusline 包一層（`jevcc → cswitch → claude-dashboard`），在下面多加 Jev 的狀態：

```
⚙ jev on · haiku (this session) · ↑✓ ↓✗ review✓ 🛡✓ inject✓ files✓ skill✓ remember✓
📊 5 prompts · ↑opus 1 · skill 2 · files 3 · 🛡 0/12 asked · inject 0/2 warned · jev $0.0008
```

`/jev statusline full|compact|off` 切換顯示；`/jev status` 列出全部設定、門檻與模型偵測來源。`jevcc uninstall` 會還原原本的 statusline。

| 防護 | hook | 實測 |
|---|---|---|
| 危險指令與寫檔先請你確認 | PreToolUse | 危險 21/22、誤擋 0/23 |
| 讀進的網頁、搜尋結果、檔案夾帶指示 AI 的文字時提醒 Claude | PostToolUse | 15/15、誤判 0/15（`jevcc injeval`） |
| 研究報告的引用：捏造、被原文反駁、原文沒提 | `/jev-cite`、`jevcc cite` | 12/12 |

skill 超過 255 個時會自動分批挑選。

## 影片中未公開的三個細節（自行依官方文件實作）

影片 15:15 提到「開關怎麼接、hook 跟 subagent 怎麼設、skill 怎麼藏才真的省 token」留給付費課程。本專案沒有參考該課程，以下依 Claude Code 官方文件的機制自行設計，並用 bench 實測：

| 細節 | 作法 | 驗證 |
|---|---|---|
| 開關 | `UserPromptSubmit` hook（`jevcc hook`）攔截 `/jev on\|off`，不呼叫模型；per-session 狀態 | 單元測試 + `/jev stats` |
| hook + subagent | `jevcc install` 一併安裝 `jev-haiku`／`jev-sonnet` 兩個輕量 subagent（`model` 固定、工具精簡、`omitClaudeMd: true`；官方說明自訂 subagent 只拿自己的 system prompt）。`"delegate": true` 時，hook 建議 Claude 交給對應的 `jev-<tier>`（`"delegate_via": "model"` 則改用一般 subagent 加 model 參數） | `jevcc bench --arms off,delegate-model,delegate-agent` |
| 藏 skill | `jevcc skills hide [--scope ...]` 把 skill 設成 `skillOverrides: "name-only"`：清單只剩名稱、每回合少送描述；Jev 讀完整描述挑 skill，並把描述附在建議裡讓 Claude 載入。`jevcc skills show` 只還原它自己加的項目 | `jevcc skilleval`：每回合輸入 token、載錯率、不該載卻載率，三組比較（原清單／只留名稱／只留名稱＋Jev） |

## 安裝

需要 Go 1.22+、Claude Code、TypeSafe API key（https://console.typesafe.ai/keys）。

```bash
cd agent-jev-integration
go install ./cmd/jevcc          # 安裝到 $(go env GOPATH)/bin，需在 PATH 上
jevcc key                       # 貼上 key（不回顯），存於 ~/.jevcc/config.json (600)；或 export TYPESAFE_API_KEY
```

安裝位置任選（可同時裝多處，同一句 prompt 只會路由一次）：

| 方式 | 指令 | 作用範圍 |
|---|---|---|
| 使用者層級 | `jevcc install` | 所有專案（`~/.claude/settings.json`、`~/.claude/commands/`） |
| 專案共享 | `jevcc install --scope project --dir <repo>` | 該 repo，進版控 |
| 專案本機 | `jevcc install --scope local --dir <repo>` | 該 repo 本機（`settings.local.json`） |
| Plugin | `claude plugin marketplace add <本目錄>` → `claude plugin install jevcc@jevcc` | 由 Claude Code plugin 管理 |
| 單次試用 | `claude --plugin-dir <本目錄>/plugin` | 只有這次 session |

`jevcc doctor` 檢查 key、claude 與各層級安裝狀態；`jevcc uninstall [--scope ...]` 移除。

## 使用

```text
/jev on        # 這個 session 開啟 Jev 路由；/jev off 關閉 → 可隨時對照
/jev stats     # Jev 判了什麼、延遲、成本、Claude 是否照建議委派／載入 skill
/jev-classify comments.csv 分成 正面/負面/產品建議/未回覆問題/已回覆問題
/jev-lint src/
/jev-review main
/jev-audit 14
```

CLI 也可直接用：`jevcc route "幫我找 hook.go"` 看單句判斷細節；`jevcc classify --in x.csv --questions q.json --rank urgency --top 20`。

設定檔 `~/.jevcc/config.json`（皆可省略）：`tiers`（預設 haiku < sonnet < opus，可改成加入 fable）、
`delegate`（false）、`min_confidence`（0.75）、`skill_gate`（0.3）／`skill_fit`（0.7）——後兩個門檻由 `jevcc eval` 的門檻掃描選定、再用獨立的 30 句 `bench/router_holdout.jsonl` 驗證、`hook_timeout_ms`（4000）、`default_on`。

## 量化證據：怎麼證明加了 Jev 有沒有用

三層，從便宜到貴，**每一層都可能得出「沒有幫助」的結論，報告會照實寫出來**。

1. **離線路由準確度**：`jevcc eval [--baseline haiku]`
   - 60 句人工標註 prompt（`bench/router_set.jsonl`，含中文，因 Jev 官方說 CJK 準確度較低）＋本機凍結的 skill 清單（`bench/roster.json`）。
   - 指標：tier 一致率（附 Wilson 95% 信賴區間）、混淆矩陣、**降級率（比標註便宜＝品質風險）與升級率（浪費錢）分開算**、
     高信心時的一致率與涵蓋率、skill 載錯率／不該載卻載率（cookbook 同一套指標）、延遲 p50/p95、每次決策成本。
   - `--baseline haiku` 用同樣題目讓 `claude -p --model haiku`（關掉工具、MCP、skills 以免灌水成本）當路由器，跟 Jev 比。
   - 限制：標註是人的判斷，不是跑出來的真值；樣本 60 句，信賴區間會寬，報告會列出。
2. **端到端 A/B**：`jevcc bench --repeat 3`
   - 10 個任務（`bench/tasks/*`：找檔、格式轉換、改名、讀碼、修 off-by-one、加功能＋隱藏測試、重構、ADR 設計、修 data race、45 筆客服單分類），
     每次在 fixture 的全新副本中執行，三組：`off`（原生）、`hook`（`/jev on` 的效果）、`route`（`jevcc ask` 的效果）。
   - **品質只看確定性的檢查指令**（隱藏測試、測試檔雜湊不可變、輸出比對、≥90% 分類正確），不用模型自評。每個檢查都已驗證「原始 fixture 會失敗、正確解會通過」。
   - 成本＝`claude -p` 回報的 `total_cost_usd`（訂閱制也以定價計）**加上** Jev 自身成本；另列時間、回合數、是否用了 subagent、各模型成本占比。
   - 與 `off` 成對比較：每次執行的成本差與 bootstrap 95% CI、品質退步／進步次數；區間含 0 就明寫「無明確差異」。
   - 執行順序隨機交錯，避免時段或快取暖機偏向某一組。這會花真的額度：先 `--dry-run`、再 `--only find-file,fix-race --repeat 1` 試跑。
3. **日常遙測**：`/jev stats`——在你真實使用中記錄每次判斷（`~/.jevcc/decisions.jsonl`），並從 transcript 驗證 Claude 是否真的照建議做。
   它回答「Jev 做了什麼、Claude 有沒有聽」，**不**宣稱省了多少錢（那要靠第 2 層）。

`classify`／`lint`／`review` 每次執行都會印出 Jev 的 token、成本與耗時。

## 開發

```bash
gofmt -l . && go vet ./... && go test ./...
go run ./cmd/jevcc plugin-files plugin   # 修改 internal/install/commands/*.md 後同步 plugin/
```

慣例同 `../agent-bridge`：Go 標準庫、`cmd/` + `internal/`、hook 永不失敗、設定檔原子寫入並保留他人 hook。
