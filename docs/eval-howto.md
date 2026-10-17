# 怎麼重跑評測

計畫與判定方式見 `docs/eval-plan.md`。以下是在自己的機器上重跑、或用自己的題目量的步驟。

## 準備

1. Go 1.22+、Claude Code、Jev 的 key（`jevcc key`），以及 Python 3、ffmpeg/ffprobe、Node。
2. Blender 4.5 LTS（Blender 題目用；Intel Mac 只能用到 4.5），設 `BLENDER` 指向執行檔，例如
   `/Applications/Blender.app/Contents/MacOS/Blender`。
3. 影片題的離線套件：`bench/open/_lib/prepare-remotion-cache.sh`（需要網路一次，之後評測不連網）。
4. 每個要用的 subscription 帳號產生一個 token：在終端機執行 `claude setup-token`，把 token 存成只有自己能讀的檔案
   （例如 `~/.jevcc-bench/<帳號>.token`，權限 600）。評測在隔離環境執行，不會用到也不會改動你的 `~/.claude`。
5. 編譯：`go build -o /tmp/jevcc ./cmd/jevcc`（不必 `go install`）。

## 執行

```bash
# 題目檢查本身是否有效（不呼叫 Claude）
/tmp/jevcc bench --validate --tasks bench/tasks,bench/hard,bench/open

# 乾淨環境會載入什麼（第一次或換 Claude Code 版本時）
/tmp/jevcc bench --probe-allow --token-file ~/.jevcc-bench/a.token

# 先看會跑幾次
/tmp/jevcc bench --dry-run --tasks bench/hard,bench/open --arms opus/off,sonnet/off,haiku/off --repeat 3

# 正式跑；多個帳號依序使用，7 日用量到 70% 換下一個，全部到上限就停，之後用 --resume 接續
/tmp/jevcc bench --tasks bench/hard,bench/open --arms opus/off,sonnet/off,haiku/off --repeat 3 \
  --token-file ~/.jevcc-bench/a.token,~/.jevcc-bench/b.token --out bench/results
/tmp/jevcc bench ... --resume bench/results/bench-<時間>.jsonl

# 盲評（模型）、盲評（人工）、報告
/tmp/jevcc judge --runs bench/results/bench-<時間>.jsonl --token-file ~/.jevcc-bench/a.token
/tmp/jevcc rate --runs bench/results/bench-<時間>.jsonl --per-task 6   # 開 rate.html 評分，匯出 ratings.json
/tmp/jevcc report --runs bench/results/bench-<時間>.jsonl --ratings ratings.json
```

組別寫法：`<主導模型>/<off|up|down|both>[/open]`，例如 `haiku/up`（haiku 主導、Jev 往上派工＋完成後審查）、
`opus/down`（opus 主導、簡單部分交給便宜模型）、`sonnet/both/open`（雙向、subagent 工具全開）。

## 用自己的題目量

在任一目錄建 `<題名>/task.json`（`prompt`、`check`、可選 `category`、`variant`、`skills`、`mcp`、`cache`）、`fixture/`（每次執行的
起始檔案）、`check.py` 或其他檢查（結束碼 0 = 通過，可印 `METRIC 名稱=數值`），以及 `testdata/solution/`（參考解，鏡像工作目錄）。
開放式題目再加 `rubric.md`（給評審）與 `human.json`（給人工評分的 5 個選項）。先跑 `--validate`，再用 `--tasks <你的目錄>` 評測。

## 評測結束後

用 `bench/envsnap.sh snap` 再拍一次設定快照，與開始前的比對（`bench/envsnap.sh diff`），確認環境沒有被改動；刪除 token 檔，
並到帳號設定撤銷 `setup-token` 產生的 token。
