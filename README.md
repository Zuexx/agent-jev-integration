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
  「委派給便宜模型的 subagent」建議**預設開啟，只限程式工作**：Jev 同一次呼叫裡多問一題「這是不是寫／改／修／測／審程式碼」，
  p ≥ 0.8 且 Jev 判定的等級比主對話便宜才建議委派（`"delegate_scope": "all"` 取消程式限制、`"delegate": false` 關閉）。
  依據是 2026-10 的隔離評測（`docs/eval-report.md`）：opus 主導開委派品質不變，只有程式類明確省錢（約 −16%）。
  這個「程式工作」判斷在 30 句沒看過的 `bench/router_holdout.jsonl` 上 29/30 正確（誤判 1 句：要求挑戰改寫計畫）。
- **Headless（`jevcc ask "<prompt>"`）**：先由 Jev 決定，再以 `claude --model <tier>` 啟動，這才是真正的逐句切換模型。
- **挑 skill**：照官方 Skill suggestion cookbook 的兩段式（第一次排名全部 skill + 「需不需要 skill」閘門，第二次精讀前三名可全部否決）。
  它降低「載錯 skill」，**不會**減少 Claude Code 本身送出的 skill 清單 token。
- 信心不足、逾時、出錯 → 不加任何內容，prompt 照常送出（hook 永不讓 claude 壞掉）。只有可執行的建議才會加進 context。
  呼叫失敗會記下來，狀態列顯示「⚠ N Jev errors」。macOS 螢幕鎖定時系統的憑證驗證會失敗（`x509: OSStatus -26276`），
  這種情況會自動改用系統內建的 `/etc/ssl/cert.pem` 再試一次。

## 建議設定：從 claude 進、主導模型照舊、讓 Jev 負責分類與程式委派

依 2026-10 的隔離評測（`docs/eval-report.md`，D9、D11）：**不要把 Claude Code 的預設模型改成 haiku**。

- 主導模型維持 opus（或你習慣的 sonnet）。研究、規劃、設計、影片、Blender 這類開放式與視覺類工作，haiku 主導明顯較差
  （opus 評審與兩位人工評分者一致）；「haiku＋往上升級」花費約 haiku 的 11 倍、opus 的一半，品質大多仍輸 opus 主導。
- 省錢靠兩件事：程式工作由 Jev 判斷後委派給便宜的 subagent（預設開，評測中程式類約 −16%、品質不變），
  以及你自己依工作挑主導模型（sonnet 主導比 opus 便宜約 65%；品質差距評測中不確定）。
- 只有規格清楚、能用測試驗收的程式工作，才考慮自己 `/model haiku`（程式難題 haiku 主導 14/15 通過，花費約 opus 的 4%）。

一行安裝（不改你的預設模型）：

```bash
jevcc install --on-by-default
```

- `--on-by-default`：每個新對話自動開啟 Jev（單一對話可用 `/jev off` 關掉）
- 主對話用什麼模型由 hook 自動偵測（對話紀錄裡最近一次回覆的模型 → Claude Code 設定），用 `/model` 中途切換也會跟著變
- `--default-model <model>` 仍可用（會改 Claude Code 的預設模型，原本的值會記下，`jevcc uninstall` 時還原），但不建議設成 haiku。

預設行為：往下委派開但只限程式工作、往上升級與事後驗收只在主對話比 opus 便宜時作用（例如你切到 haiku／sonnet）、危險動作檢查**關**（`/jev guard on` 或 `jevcc install --guard` 開啟，只會要求確認）、subagent 用 MCP／skill **關**（`/jev mcp on` 或 `jevcc install --agent-mcp` 開啟，新對話生效）。要改才需要 `~/.jevcc/config.json`，欄位見下方設定說明。

每句話的處理方式（「實測」欄是舊的 bench 數字，依 D6 不當公開依據，保留作為參考；升級與驗收兩列只在主對話比 opus 便宜時發生）：

| 情況 | 會發生什麼 | 實測 |
|---|---|---|
| 程式工作，Jev 判定比主對話便宜的等級就夠 | 主對話把工作交給 `jev-haiku`／`jev-sonnet` subagent（`"delegate": false` 關閉） | 隔離評測：opus 主導程式難題約 −16%、品質不變 |
| Jev 判定需要 opus（規劃、架構、不明原因的 bug、安全審查） | 便宜的主對話先把工作交給 `jev-opus` subagent | 17/17 照做；opus 題中 Jev 判對 17/21 |
| haiku 說做完了 | Jev 驗收，有改程式卻沒跑過測試、有沒解決的錯誤等狀況就交給 opus 複查（`"verify": true`） | 簡單任務誤升級 0/2 |
| 要執行指令或寫檔（開啟 guard 時） | 危險動作改成請你確認（`"guard": true`，預設關） | 危險抓到 21/22，安全誤擋 0/23（字串規則：12/22、2/23） |
| 問題跟專案程式碼有關 | 附上最可能相關的 3 個檔案 | 前三名挑對 19/20；haiku 讀的 token −20% |
| 你說出長期偏好或規則 | 提醒 Claude 存進記憶 | 20/20 抓到、一次性指示誤判 0/20 |

Jev 比較會判錯的題型：「找資料寫研究」和「整理領域用語」。遇到時自己 `/model opus`。

驗證指令：`jevcc escalateval`、`jevcc guardeval`、`jevcc findeval`、`jevcc memeval`、`jevcc bench --tasks bench/hard|bench/nav`。

## statusline 與防護

安裝時會把現有的 statusline 包一層（`jevcc → cswitch → claude-dashboard`），在下面多加 Jev 的狀態：

```
⚙ jev on · opus (this session) · ↑✗ ↓✓ review✗ 🛡✗ mcp✗ inject✓ files✓ skill✓ remember✓
📊 5 prompts · ↓cheaper 1 · skill 2 · files 3 · inject 0/2 warned · jev $0.0008
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

需要 Claude Code 與 TypeSafe API key（https://console.typesafe.ai/keys）。三種裝法任選：

```bash
# 1. 下載編好的執行檔（macOS／Linux／Windows，amd64／arm64）。repo 目前是 private，用 gh（已登入、有權限）下載：
gh release download -R Zuexx/agent-jev-integration -p '*darwin_arm64.tar.gz' -p checksums.txt   # Apple Silicon；Intel 用 darwin_amd64
shasum -a 256 -c checksums.txt --ignore-missing && tar -xzf jevcc_*_darwin_arm64.tar.gz jevcc
sudo mv jevcc /usr/local/bin/   # 或放在任何 PATH 上的目錄
#    repo 公開後也可直接下載：https://github.com/Zuexx/agent-jev-integration/releases/latest
#    （用瀏覽器下載時 macOS 會擋未簽章的執行檔：xattr -d com.apple.quarantine jevcc）

# 2. 有 Go 1.22+（private repo 需先 export GOPRIVATE=github.com/zuexx/* 並設好 git 的 GitHub 認證）：
go install github.com/zuexx/agent-jev-integration/cmd/jevcc@latest

# 3. 從原始碼：
cd agent-jev-integration && go install ./cmd/jevcc

jevcc version
jevcc key                       # 貼上 key（不回顯），存於 ~/.jevcc/config.json (600)；或 export TYPESAFE_API_KEY
```

發新版：在 main 上打 `vX.Y.Z` tag 並推上去，`.github/workflows/release.yml` 會用 GoReleaser 編好六個平台並附到 GitHub Release。

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
`delegate`（true）、`delegate_scope`（`code`：只委派程式工作；`all`）、`min_confidence`（0.75）、`skill_gate`（0.3）／`skill_fit`（0.7）——後兩個門檻由 `jevcc eval` 的門檻掃描選定、再用獨立的 30 句 `bench/router_holdout.jsonl` 驗證、`hook_timeout_ms`（4000）、`default_on`。

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
3. **日常遙測**：`/jev stats`——在你真實使用中記錄每次判斷（`~/.jevcc/decisions-YYYY-MM-DD.jsonl`，每天一個檔，預設保留 14 天），並從 transcript 驗證 Claude 是否真的照建議做。
   它回答「Jev 做了什麼、Claude 有沒有聽」，**不**宣稱省了多少錢（那要靠第 2 層）。

`classify`／`lint`／`review` 每次執行都會印出 Jev 的 token、成本與耗時。

## 開發

```bash
gofmt -l . && go vet ./... && go test ./...
go run ./cmd/jevcc plugin-files plugin   # 修改 internal/install/commands/*.md 後同步 plugin/
```

慣例同 `../agent-bridge`：Go 標準庫、`cmd/` + `internal/`、hook 永不失敗、設定檔原子寫入並保留他人 hook。
