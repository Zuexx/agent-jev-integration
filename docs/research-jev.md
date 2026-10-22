# 研究：Jev 能力 × Claude Code 掛點，找「裝了就省 token」的做法

日期：2026-10-19　本機 Claude Code：`2.1.303 (Claude Code)`（`command claude --version`）；官方 changelog 最新一版也是 2.1.303（2026-10-18）（https://code.claude.com/docs/en/changelog.md）。

寫法：**事實**一律附來源 URL；標 **〔推論〕** 的是我的判斷，沒有實測。背景見 `docs/decisions.md` D9–D12。

---

## 0. 結論先講

1. **D12 的前提「hook 換不了主對話的模型」要修正一半**：settings hook（shell 指令那種）確實換不了；但 Claude Code 的 **mods**（plugin 內的 JS 事件處理器）在 `turn.step` 可以對「每一次送給模型的請求」改 `model` 或 `effort`（https://code.claude.com/docs/en/plugins/mods/reference.md 「Turns」表；https://code.claude.com/docs/en/plugins/mods/events.md 「Follow a turn」）。
2. **改 effort 在 5.5 世代模型上不會打掉 prompt cache；改 model 會**（https://code.claude.com/docs/en/prompt-caching.md 「Changing effort level」「Switching models」）。所以「Jev 逐句決定 effort」是目前最有希望的被動省錢點；「Jev 逐句換模型」仍因 cache 失效而不划算（只在 cache 已冷時例外）。
3. **派 subagent 時改它的模型**現在做得到而且看得到真正的任務文字：PreToolUse 的 `updatedInput` 會整個替換工具參數，而 Agent 工具的參數裡有 `model`（https://code.claude.com/docs/en/hooks.md 「PreToolUse decision control」「Agent」）；mods 的 `agent.spawn` 也能直接指定模型（https://code.claude.com/docs/en/sub-agents.md 「Choose a model」）。這解掉 D12 第 3 點「Jev 只看得到使用者的 prompt」。
4. **工具輸出可以縮**：PostToolUse 的 `updatedToolOutput` 對所有工具（含 Read/Bash/Grep）都有效（https://code.claude.com/docs/en/hooks.md 「PostToolUse decision control」）。Jev 的「逐行相關性」cookbook 正好能決定留哪幾行。但已有不用 Jev 的確定性開關（`bashOutputMaxChars` 最低 4000），要先證明 Jev 版比它好。
5. Jev **不能**摘要／壓縮文字（不生成文字），也不適合每回合動態改 skill／tool 清單（會打掉 cache）。

---

## Part 1 — Jev 官方文件（https://docs.typesafe.ai/llms.txt 全部連結已讀）

### 1.1 Jev 是什麼、不是什麼

- 不是 coding agent 的 LLM，不生成文字、不寫程式、不對話；沒有 `model: "jev-latest"` 這種設定能讓 coding agent 換成 Jev。（https://docs.typesafe.ai/introduction/coding-agents.md）
- 輸入 `state`（字串／JSON 物件／陣列）＋一組具型別的問題，回傳結構化答案；同一請求內所有問題**並行、各自獨立**評估，「Adding questions barely changes the response time」，也不會互相造成 context rot。（https://docs.typesafe.ai/introduction.md）
- 設計理念：程式掌控流程，模型只做窄的判斷；「Avoid agent `while` loops when a software workflow can express the same behavior」；多數查詢約 100 ms。（https://docs.typesafe.ai/concepts/how-to-build-with-system-one.md）
- System One 名稱來自 Kahneman 的 System 1/System 2：快速直覺判斷；需要長推理的就拆成多個原子問題，或升級給「a person or a reasoning model」。（https://docs.typesafe.ai/concepts/system-one.md）
- 以 RLCD 訓練，回傳校準過的機率；校準是群體層級，不保證單一答案正確。（https://docs.typesafe.ai/introduction/machine-learning-primer.md）

### 1.2 問題型別（primitives）

| 型別 | 回傳 | 限制／重點 | 來源 |
|---|---|---|---|
| Choice | `choice`、`probabilities`、`confidence` | 最多 255 個選項；機率總和為 1（所以一定會選一個，要另配 Noul 判斷「有沒有」） | https://docs.typesafe.ai/api.md 、https://docs.typesafe.ai/cookbooks/semantic_find.md |
| Score | `score`、`legend`、`probabilities`、`confidence` | 至少 2 級、API 最多 10 級；score 是機率加權值，不要拿來內插精確數字 | https://docs.typesafe.ai/api.md 、https://docs.typesafe.ai/model-jaggedness/jev-1.13.md |
| Noul | `noul`（0–1） | 沒有 `confidence`；可用 \|2p−1\| 自算 | https://docs.typesafe.ai/primitives.md 、https://docs.typesafe.ai/confidence.md |
| Box | 圖片中目標的四邊（0–1） | 「Dreamer-ready」（非正式）、需 `TypeSafe-Danger-Zone-Images: true` header 與 Jev 1.14；沒有「找不到」答案 | https://docs.typesafe.ai/primitives/box.md |

- `instructions`／`criteria` 可用 JSON 結構，用反引號路徑（如 `` `support.tickets[0].message` ``）指向 state 的欄位。（https://docs.typesafe.ai/primitives/advanced.md 、https://docs.typesafe.ai/primitives.md）
- 問題 ID 不會送進模型。（https://docs.typesafe.ai/api.md）
- 問題之間沒有依賴；只有「第二個請求的 state 必須等第一個答案才組得出來」時才分兩次。（https://docs.typesafe.ai/primitives.md 「When one question depends on another」）
- 圖片：可放在 state、instructions、criteria；同樣是「Dreamer-ready」、要 header；圖片 token 照文字價計費。（https://docs.typesafe.ai/images.md）注意 System One 頁仍寫「Jev currently accepts text input only」（https://docs.typesafe.ai/concepts/system-one.md），文件前後不一致。

### 1.3 Confidence

- `confidence` 由 probabilities 推得：Choice 為 (p_max − 1/n)/(1 − 1/n)；Score 另外考慮離峰值的距離；Noul 沒有。（https://docs.typesafe.ai/confidence.md）
- 建議三段：高信心自動做、中信心確認、低信心不做／交給人或別的系統；門檻依風險而定。（同上；https://docs.typesafe.ai/patterns/confidence-routing.md）
- 也建議試 top probability 與 top-to-second ratio。（https://docs.typesafe.ai/confidence.md）

### 1.4 模型、價格、限制、延遲

- `jev-1.14.0`、`jev-1.13.0`；每百萬輸入 token $0.042，**輸出免費**；context：每請求 64k、每問題 32k（state＋單一問題）。（https://docs.typesafe.ai/models.md）
- `jev-latest` → 1.13.0，`jev-preview` → 1.14.0（1.14 自 2026-10-14 起）；別名會移動，調過門檻的應鎖版本。（同上）
- Long context：送 `TypeSafe-Danger-Zone-Long-Context: true` 後兩個上限都變 262k，同價但「slower and less accurate」。（同上）
- 問題數沒有上限，只受 context 限制。（同上）
- Rate limit 依組織，以 token/s 與 req/s 計，超過回 429；SDK 自動退避重試。（同上；https://docs.typesafe.ai/api.md）
- 請求 body 最多 32 MiB、巢狀最多 64 層；可用 gzip／zstd 壓縮 body（這是傳輸壓縮，不是 prompt 壓縮）。（https://docs.typesafe.ai/api.md）
- Python SDK 預設 timeout 10 秒。（https://docs.typesafe.ai/sdk/python/api/constants.md）
- **語言**：英文最好；「Other languages, including CJK scripts, are handled but not equally well」。（https://docs.typesafe.ai/models.md）〔推論〕使用者常用中文下 prompt，逐句分類的準確度要另外量。
- **快取**：Jev 文件沒有任何伺服器端快取或 prompt caching 機制；每次請求都照 state 全額計費（parallel_questions 就是靠「state 只送一次」省錢）。（https://docs.typesafe.ai/cookbooks/parallel_questions.md；全站搜尋 cache 只出現 cookbook 的本地 `JsonCache`）
- 不以客戶資料訓練；企業可 ZDR。（https://docs.typesafe.ai/models.md 、https://docs.typesafe.ai/legal.md）
- 不做 fine-tune／LoRA；客製只能透過 state、instructions、criteria。（https://docs.typesafe.ai/models.md）

### 1.5 Jev 明確做不好／做不到的事（jev-1.13 jaggedness）

字面理解、數學與計數、日期比較、多層間接、**state 裡大量無關內容會降準確度（context rot）**、對抗性內容（prompt injection 會影響答案）、instructions 與 criteria 矛盾、**Choice 偏向第一個選項**、生成文字。（https://docs.typesafe.ai/model-jaggedness/jev-1.13.md）
另：「Jev is not a drop-in replacement for the LLM behind Claude Code」。（https://docs.typesafe.ai/introduction/coding-agents.md）

### 1.6 Patterns

| Pattern | 摘要 | 來源 |
|---|---|---|
| Speculative fan-out | 把可能用到的問題全部塞同一請求，程式事後挑；省來回 | https://docs.typesafe.ai/patterns/fan-out.md |
| Confidence-gated routing | 答案決定做什麼，confidence 決定要不要做；高風險動作門檻更高 | https://docs.typesafe.ai/patterns/confidence-routing.md |
| Composite scoring | 拆成多個 Score，程式加權 | https://docs.typesafe.ai/patterns/composite-scoring.md |
| Intent routing | 先分類意圖與複雜度，再交給確定性程式／專門 LLM／人；「the expensive resources only get invoked for the requests that actually need them」 | https://docs.typesafe.ai/patterns/intent-routing.md |

### 1.7 Cookbooks（全部）

**LLMs and agents**
- **Function calling**：自然語言指令 → 函式名與封閉集合參數各一個 Choice；`stated` Noul 決定參數要不要省略；confidence 取最不確定的那個。（https://docs.typesafe.ai/cookbooks/function_calling.md）
- **Skill suggestion**：182 個 skill，第一請求用一個 Choice 排名＋3 個 Noul 判斷「需不需要 skill」（平均 < 0.30 就不建議）；第二請求只看前 3 名的完整描述與 SKILL.md 開頭，可全部否決。建議只加一行到 system prompt，**放在 roster 之後讓 roster 文字每回合相同以保住 prefix cache**。haiku-4.5 上錯誤載入 16.8% → 7.3%、不該載入卻載入 9.8% → 4.0%（488 個請求）。也會把部分原本答對的帶錯。（https://docs.typesafe.ai/cookbooks/skill_suggestion.md）
- **Guardrails for LLMs**：一個請求內一組 hazard Noul＋一個嚴重度 Score，輸入輸出兩邊都檢查；政策是程式裡的門檻。（https://docs.typesafe.ai/cookbooks/llm_guardrails.md）
- **Double-checking citations**：先字串比對找不存在的引文，再用一個 Choice 判斷上下文是否支持主張；8 則中 4 則正確的 confidence ≥ 0.93，4 則造假全抓到。（https://docs.typesafe.ai/cookbooks/citation_check.md）
- **Classifying RAG passages**：每段 passage 一個請求、4 個 Noul（相關、含答案證據、與前提矛盾、含 prompt injection），程式依序判斷納入／列為衝突／丟掉；每個 bar 至少三分之二被排除；明說「Nothing here is a security boundary」；成本隨 k 線性成長。（https://docs.typesafe.ai/cookbooks/classifying_rag_passages.md）
- **Simple memory system**：每次呼叫後用一個 Noul gate 判斷值不值得記（≥ 0.5），之後把記憶加進 state；路由正確從 2/6 → 6/6；記憶只增不減、會強化錯誤。（https://docs.typesafe.ai/cookbooks/basic_memory.md）

**Classification**
- **Hierarchical classification**：逐層 Choice，beam search 用幾何平均機率保留 K 條路徑。（https://docs.typesafe.ai/cookbooks/hierarchical_classification.md）
- **Classification using confidence**：SEC 報告 75 類；高信心那半 90% 正確，另一半 40%，退一層報告後升到 70%。（https://docs.typesafe.ai/cookbooks/classification_using_confidence.md）
- **Twitch chat filter**：每則訊息一請求、三個問題，最多 8 則並行。（https://docs.typesafe.ai/cookbooks/twitch_chat_filter.md）

**Extraction**
- **SDE cascade**：便宜模型抽取 → Jev 逐欄位 Noul 驗證（「bad = true」、用 max 聚合）→ 有旗才升級到推理模型；cascade 的 Pareto 前緣在所有單一模型的左上方（內部結果、jev-1.12 時期的價格）。（https://docs.typesafe.ai/cookbooks/sde_cascade.md）
- **Date extraction**：日期各部分用 Choice 抽，計算在程式。（https://docs.typesafe.ai/cookbooks/date_extraction_cookbook.md）
- **Pre-parsed value extraction**：regex 先找候選，Jev 只挑一個。（https://docs.typesafe.ai/cookbooks/pre_parsed_value_extraction_cookbook.md）
- **Structure recovery**：兩個請求重建 Markdown；10,211 tokens、0.8 秒、$0.0015。（https://docs.typesafe.ai/cookbooks/autoformat.md）

**Search and matching**
- **Re-ranking**：BM25 取 30 候選後 Jev 重排，top-1 5% → 18%、top-10 38% → 62%。（https://docs.typesafe.ai/cookbooks/rerank_typesafe.md）
- **Line-by-line search**：文件每行加 ID，一個 Choice 對行 ID 排名（≤ 255 行，超過就兩段）＋一個 Noul 判斷「文件裡到底有沒有答案」。（https://docs.typesafe.ai/cookbooks/semantic_find.md）
- **Entity alignment**：一個 Score＋三個 Noul 判斷是否同一實體。（https://docs.typesafe.ai/cookbooks/entity_alignment.md）

**Techniques**
- **Self-consistency: choices**：15 次重複，Jev 一致率 90.8% → 99.2%（加 uncertain 選項，25.8% 判不確定）；平均延遲 114 ms。（https://docs.typesafe.ai/cookbooks/consistency_choice_cookbook.md）
- **Self-consistency: nouls**：14 題 Noul 重複 15 次，平均延遲 111 ms。（https://docs.typesafe.ai/cookbooks/consistency_noul_cookbook.md）
- **Parallel questions**：13 題一起問 vs 分 13 次，答案不變，cookbook 索引寫「12.2x cheaper and 10.0x faster」（https://docs.typesafe.ai/cookbooks.md），primitives 頁寫「11.5x cheaper and 9.6x faster」（https://docs.typesafe.ai/primitives.md）——文件數字不一致。
- **Autoresearch feature discovery**：LLM 提問、Jev 答成數值特徵、CatBoost 訓練並回饋。（https://docs.typesafe.ai/cookbooks/autoresearch_feature_discovery.md）
- Demo **Smart home**：speculative fan-out，泛用問題才 fallback 給 LLM，「adds negligible latency」。（https://docs.typesafe.ai/demos/smart-home.md）

### 1.8 與本題直接相關的 Jev 官方說法

- Use case map 明列「Use Jev queries to make your harness smarter - model routing, semantic context retrieval, LLM error detection and guardrails」、「Select useful context for downstream AI workflows」、「Escalate requests that need a more expensive model」。（https://docs.typesafe.ai/concepts/use-case-map.md）
- **Prompt／context 壓縮或摘要**：Jev 文件沒有這種功能；唯一的「compression」是 HTTP body 的 zstd/gzip。（https://docs.typesafe.ai/api.md）Jev 能做的是「選」：選相關段落（RAG cookbook）、選相關行（semantic_find）、選 skill（skill_suggestion）。
- **Early stopping／要不要做這一步**：文件的做法是 Noul gate（skill_suggestion 的「要不要 skill」、semantic_find 的「有沒有答案」、memory 的「值不值得記」）。
- **Agent loops**：官方立場是避免 agent while loop，Jev 不選下一步。（https://docs.typesafe.ai/concepts/how-to-build-with-system-one.md）
- **成本宣稱**：首頁「193.6x Faster, 444.6x Cheaper」（註明 based on workflows for System One tasks）、輸入價比 Claude Fable 5.1 低 238 倍、Jack & Jill 案例「Reduced cost by 88%」。（https://typesafe.ai）〔推論〕這些比的是「把判斷步驟從 LLM 換成 Jev」，不是 coding agent 整體省錢。

---

## Part 2 — Claude Code 掛點（code.claude.com/docs，對應 v2.1.303）

### Q1. PreToolUse 能改工具參數嗎？能改 subagent 的 `model` 嗎？

- **能改參數**：「`updatedInput` | Modifies the tool's input parameters before execution. Replaces the entire input object, so include unchanged fields alongside modified ones.」（https://code.claude.com/docs/en/hooks.md 「PreToolUse decision control」）
- **Agent 工具的參數有 `model`**：「`model` | string | `"sonnet"` | Optional model alias to override the default」。（同頁「Agent」）文件沒有把 Agent 排除在 `updatedInput` 之外，所以**依文件可以**；但我沒實測。
- 會被覆蓋的情況：`availableModels` 等會讓實際模型不同，結果的 `resolvedModel` 會顯示真正用的模型。（同頁）`CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1` 時「Claude can't pass a model when it starts a subagent」。（https://code.claude.com/docs/en/sub-agents.md）
- **mods 版更直接**：`agent.spawn`「`next({ ...e, model })` to choose its model, or `{ deny: reason }`」（https://code.claude.com/docs/en/plugins/mods/reference.md 「Subagents」），且「If an installed mod sets a model in its `agent.spawn` hook, Claude Code uses that model in place of the per-invocation parameter.」（https://code.claude.com/docs/en/sub-agents.md 「Choose a model」）
- Agent 呼叫還可帶 `effort` 參數（v2.1.292+）。（https://code.claude.com/docs/en/sub-agents.md）
- PostToolUse 的 Agent `tool_response` 有 `resolvedModel`、`totalTokens`（只算最後一個請求）、`usage`。（https://code.claude.com/docs/en/hooks.md 「Agent」）

### Q2. PostToolUse 能取代／縮小工具輸出嗎？

- **能，所有工具**：「`updatedToolOutput` | Replaces the tool's output with the provided value before it is sent to Claude. The value must match the tool's output shape」；「`updatedMCPToolOutput` | Replaces the output for MCP tools only. Prefer `updatedToolOutput`, which works for all tools」。（https://code.claude.com/docs/en/hooks.md 「PostToolUse decision control」）
- 注意：「For built-in tools, a value that doesn't match the tool's output schema is ignored」；「Stripping error details that Claude needs can cause it to proceed on a false assumption.」（同上）
- PreToolUse 也能在執行前改指令（官方範例：把測試指令改成只輸出失敗）。（https://code.claude.com/docs/en/costs.md 「Offload processing to hooks and skills」）
- 內建上限：Bash 成功輸出 inline 約 30,000 字元，超過就存檔給路徑＋前 2,000 字預覽（https://code.claude.com/docs/en/tools-reference.md 「Output limits」）；`bashOutputMaxChars` 可設在 4,000–128,000（https://code.claude.com/docs/en/settings-reference.md）；MCP 預設上限 25,000 tokens（`MAX_MCP_OUTPUT_TOKENS`），超過 50,000 字元存檔（https://code.claude.com/docs/en/mcp.md 「MCP output limits」）。
- mods 的 `session.append` 能改寫存進對話的列，包括 `tool_result` 的 `content`。（https://code.claude.com/docs/en/plugins/mods/reference.md 「Session」）

### Q3. 各事件能回傳什麼？能觸發 /compact、改模型／effort／權限嗎？

| 事件 | 能做的事 | 來源 |
|---|---|---|
| 全部 | `continue:false`＋`stopReason`、`systemMessage`（給使用者看）、`terminalSequence`；`additionalContext`／`systemMessage` 每個 10,000 字上限 | https://code.claude.com/docs/en/hooks.md 「JSON output」 |
| SessionStart | `additionalContext`、`initialUserMessage`（僅 `-p`）、`sessionTitle`、`watchPaths`、`reloadSkills`；不能擋 | 同上「SessionStart decision control」 |
| UserPromptSubmit | `decision:"block"`、`additionalContext`、`sessionTitle`；**不能改寫 prompt** | 同上「UserPromptSubmit decision control」 |
| PreToolUse | allow／deny／ask／defer（defer 只在 `-p`）、`updatedInput`、`additionalContext` | 同上 |
| PermissionRequest | allow／deny、`updatedInput`、套用權限規則 | 同上「Decision control」表 |
| PostToolUse | `decision:"block"`（只加說明）、`additionalContext`、`updatedToolOutput` | 同上 |
| Stop／SubagentStop | `decision:"block"` 讓它繼續；`additionalContext`；最多連續 8 次 | 同上「Stop decision control」 |
| SubagentStart | 只能加 `additionalContext` 到 subagent，不能擋 | 同上「SubagentStart」 |
| PreCompact | 可擋壓縮（exit 2 或 `decision:"block"`）；不能改摘要；`systemMessage`、`continue` 被丟棄 | 同上「PreCompact」 |
| PostCompact | 只讀 `compact_summary`，無決策 | 同上 |
| PreModelSwitch | 只對使用者／客戶端要求的切換：allow／deny／ask；輸入有 `context_tokens`、`estimated_cache_write_usd` | 同上「PreModelSwitch」 |
| PostModelSwitch | `additionalContext` | 同上 |
| Notification | 無決策，只能副作用 | 同上「Decision control」表 |

- **settings hook 能不能觸發 /compact、改模型、改 effort**：文件沒有任何欄位可以；`/compact` 也不在 Skill tool 可呼叫的內建指令內（「Other built-in commands such as `/compact` are not」，https://code.claude.com/docs/en/skills.md）。PreModelSwitch 只能**擋**使用者發起的切換。（https://code.claude.com/docs/en/hooks.md）
- **mods 可以**：`turn.step`「send it to a different model with `next({ ...e, model })`」，也可 `next({ ...e, effort })`（https://code.claude.com/docs/en/plugins/mods/events.md 、https://code.claude.com/docs/en/plugins/mods/reference.md）；`session.compact` 可 `{ skip }`；`$.session.compact` 存在；`$.http.fetch` 可呼叫外部 API（Jev）；`$.session.messages()` 讀 transcript；`$.model.classify`／`complete` 用使用者自己的帳號呼叫 Claude。（https://code.claude.com/docs/en/plugins/mods/reference.md 「Mods API methods」、https://code.claude.com/docs/en/plugins/mods/api.md）
- mods 的限制：單一 hook 自身執行 10 秒；`$.http.fetch` 30 秒；`claude -p` 與 Agent SDK 也會跑 hooks。（https://code.claude.com/docs/en/plugins/mods/reference.md 「Limits」、https://code.claude.com/docs/en/plugins/mods/overview.md 「Where mods run」）組織的 managed settings 可擋 mods（`allowManagedModsOnly`、`disableAllHooks`）。（同 reference）

### Q4. 能不能隱藏／延後 skill 或 MCP 工具來減少每回合 context？

- Skill：`disable-model-invocation: true`「Description not in context」；`skillOverrides` 四態 `on`／`name-only`／`user-invocable-only`／`off`；listing 預設佔 context window 的 1%（`skillListingBudgetFraction`），超過時先拿掉最少用的描述；每項描述上限 1,536 字。（https://code.claude.com/docs/en/skills.md 、https://code.claude.com/docs/en/settings-reference.md）
- MCP：tool search 預設開，「Only tool names and server instructions load at session start」；`ENABLE_TOOL_SEARCH`＝unset／`true`／`auto`／`auto:N`／`false`。（https://code.claude.com/docs/en/mcp.md 「Scale with MCP tool search」）
- 每回合改變工具清單會失效 cache：「the cache invalidates when the set of tool definitions in the request changes between turns」，tool search 開時例外。（https://code.claude.com/docs/en/prompt-caching.md）
- hook 能否每個 session 影響：settings hook 只有 SessionStart `reloadSkills`（重新掃描目錄）（https://code.claude.com/docs/en/hooks.md）；mods 有 `tool.describe`（可設 `isDeferred`）、`skill.prompt`、`prompt.section`（`{ text: null }` 省略某段 system prompt）、`prompt.attachment`（省略 reminder）、`agent.offer`（不提供某 subagent 類型）。官方警告：「Text from these hooks that changes between requests invalidates the prompt cache」。（https://code.claude.com/docs/en/plugins/mods/reference.md 、https://code.claude.com/docs/en/plugins/mods/events.md）

### Q5. 內建的被動省錢開關

- **Effort**：Opus 5.5／Sonnet 5.5／Haiku 5.5 預設 `medium`，其他支援的模型預設 `high`；thinking token 以 output 計費。（https://code.claude.com/docs/en/model-config.md 「Adjust effort level」、https://code.claude.com/docs/en/costs.md 「Adjust extended thinking」）
- **改 effort 不打掉 cache（5.5 世代＋Fable 5.1，API key 或訂閱）**：「changing effort keeps the cache, and Claude Code applies the new level without asking」；Bedrock／Vertex／gateway 例外。（https://code.claude.com/docs/en/prompt-caching.md 「Changing effort level」）
- **改模型一定打掉 cache**：「Switching models recomputes the entire request even when the content is identical」。（同上）PreModelSwitch 範例：182,340 tokens 的 context 換到 Opus 估 cache 寫入 $1.14。（https://code.claude.com/docs/en/hooks.md）
- `opusplan`：plan mode 用 opus、執行用 sonnet；每次切換都是一次 model switch、cache 重來。（https://code.claude.com/docs/en/model-config.md 、https://code.claude.com/docs/en/prompt-caching.md）
- Subagent 模型：解析順序 per-invocation `model` → 定義的 `model` → `CLAUDE_CODE_SUBAGENT_MODEL` → 主對話模型；mods `agent.spawn` 優先於 per-invocation；`CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1` 強制一種。（https://code.claude.com/docs/en/sub-agents.md）
- **Advisor tool**（實驗性）：便宜主模型＋強 advisor，Claude 自己決定何時諮詢。（https://code.claude.com/docs/en/advisor.md）
- Cache TTL：`promptCacheTtl`、`subagentPromptCacheTtl`。（https://code.claude.com/docs/en/prompt-caching.md）
- Idle compaction：約 54 分鐘閒置且 context ≥ 200k 時在 cache 過期前壓縮。（https://code.claude.com/docs/en/settings-reference.md 「idleCompaction」）
- 其他官方建議：`/clear`、CLAUDE.md 少於 200 行、把專門指令搬到 skill、關不用的 MCP、code intelligence plugin、把冗長操作丟 subagent。（https://code.claude.com/docs/en/costs.md）

---

## Part 3 — 綜合：Jev 能力 × Claude Code 掛點

排序依「預期被動省錢」。每項分開寫事實依據與推論。

### 第 1 名：每回合由 Jev 決定 effort（mods `turn.step`）

- 機制：mod 在 `turn.start` 或第一個 `turn.step`（`e.agentId` 不存在＝主對話）用 `$.http.fetch` 呼叫 Jev，state＝使用者 prompt＋最近一兩則訊息（`$.session.messages()`），一個 Score／Choice 問「這回合需要多深的推理」→ `next({ ...e, effort })`。
- 事實依據：`turn.step` 可改 effort（https://code.claude.com/docs/en/plugins/mods/reference.md）；5.5 世代改 effort 不失效 cache（https://code.claude.com/docs/en/prompt-caching.md）；thinking 以 output 計費、可達每請求數萬 token（https://code.claude.com/docs/en/costs.md）。
- 省在哪：主對話的 **output（thinking）token**。
- 品質風險：把難題判成簡單 → 少想、錯更多。建議只往下調（預設 medium → low），不往上調到 xhigh/max（官方說 max「prone to overthinking」，https://code.claude.com/docs/en/model-config.md）。
- 被動：是，裝了就作用。
- 〔推論／未驗證〕省多少不知道；要先量 low vs medium 在「簡單回合」的 output token 差與品質；中文 prompt 的分類準確度（Jev 對 CJK 較弱，https://docs.typesafe.ai/models.md）；每回合多 ~0.1–0.4 秒延遲；jevcc 是 Go，mod 必須是 JS/TS，要新寫一個 plugin。與 D12 的「haiku 先理解」問題無關，因為模型不變。

### 第 2 名：派 subagent 時由 Jev 降模型（PreToolUse `updatedInput` 或 mods `agent.spawn`）

- 機制：攔 Agent 呼叫，state＝Claude 寫的 `prompt`＋`subagent_type`，問難度／是否純查找 → 簡單的改成 `haiku`／`sonnet`（也可配 `effort`）。
- 事實依據：見 Q1。重點是**這時 Jev 看得到真正派出去的任務**，不像 D12 只看到「OK, go」。subagent 類型不變，所以它原本的工具（含 MCP）不變，避開 D3/D10 的工具問題。
- 省在哪：subagent 的 input＋output。
- 品質風險：subagent 做不好時主對話要重做（反而更貴）；主對話（opus）照樣審查結果。
- 被動：是。
- 〔推論／未驗證〕Agent 上 `updatedInput` 是否真的生效要實測（看 PostToolUse 的 `resolvedModel`）；省多少取決於使用者多常用 subagent（Explore 等內建 subagent 本來就可能用小模型，https://code.claude.com/docs/en/sub-agents.md 「Built-in subagents」）。

### 第 3 名：大型工具輸出只留相關行（PostToolUse `updatedToolOutput` 或 mods `session.append`）

- 機制：輸出超過 N 字（例如 4k）時，依 Line-by-line search cookbook：每行加 ID、一個 Choice 對行排名＋一個 Noul「有沒有答案」，state 帶「目前的任務」（使用者 prompt＋Claude 的工具意圖）；只回傳高分行＋「完整輸出在某檔案」的說明。
- 事實依據：`updatedToolOutput` 對所有工具有效（https://code.claude.com/docs/en/hooks.md）；Jev 的 semantic_find（https://docs.typesafe.ai/cookbooks/semantic_find.md）與 RAG 篩選（https://docs.typesafe.ai/cookbooks/classifying_rag_passages.md）；官方本來就建議用 hook 預先過濾（https://code.claude.com/docs/en/costs.md）。
- 省在哪：主對話 input——這段結果在之後每個請求都會以 cache read 重讀（https://code.claude.com/docs/en/costs.md 「Why usage climbs」）。
- 品質風險：高。官方警告刪掉需要的資訊會讓 Claude 錯誤假設（https://code.claude.com/docs/en/hooks.md）；Jev 對「目前任務」的理解有限、字面判斷、對大段無關內容準確度下降（https://docs.typesafe.ai/model-jaggedness/jev-1.13.md）；Choice 最多 255 行要分段。
- 被動：是。
- 〔推論〕不用 Jev 的對照組很強：`bashOutputMaxChars: 4000` 加上 Claude 自己 grep 存檔。要先證明 Jev 版比這個好；Read 的輸出通常是 Claude 主動要看的檔，縮掉風險最大，建議只對 Bash／MCP 的長輸出做。

### 第 4 名：只在 cache 已冷時由 Jev 換主模型（mods `turn.step` 的 `model`）

- 機制：只在 session 第一個回合、或距上次請求超過 cache TTL 時，才依 Jev 判斷換模型；cache 熱的時候不動。
- 事實依據：換模型會整個重算（https://code.claude.com/docs/en/prompt-caching.md）；cache 過期後本來就要重算，所以那時換沒有額外失效成本（同頁「Cache lifetime」）。
- 省在哪：主對話 input＋output（整回合用便宜模型）。
- 風險：D12 第 2 點仍在（弱模型先理解、定題目）；訂閱的主對話 TTL 是 1 小時，冷 cache 的時機不多。
- 被動：是。〔推論〕適用面窄，預期省得少；換錯模型時使用者會很困惑（要在狀態列顯示）。

### 第 5 名：任務切換時提示 `/clear`（UserPromptSubmit）

- 機制：Noul「這個 prompt 和之前的工作無關」高時，用 `systemMessage` 提醒使用者 `/clear`；context 大時才提示（mods 有 `$.session.usage()` 可讀 context 大小）。
- 事實依據：官方建議換任務時 `/clear`，「`/clear` costs nothing」（https://code.claude.com/docs/en/costs.md）；hook 不能自己觸發 `/compact` 或 `/clear`（Q3）。
- 被動：**否**，要使用者動作。mods 雖有 `$.session.compact`，但自動壓縮會失去細節，不建議自動做。

### 不可行或不划算

- **Jev 摘要／壓縮 prompt 或對話**：不可行，Jev 不生成文字（https://docs.typesafe.ai/model-jaggedness/jev-1.13.md 「Generation」）；PreCompact 也改不了摘要（https://code.claude.com/docs/en/hooks.md）。
- **每回合依 Jev 結果增減 skill／tool 清單或 system prompt 段落**：會使 cache 失效（https://code.claude.com/docs/en/prompt-caching.md 、https://code.claude.com/docs/en/plugins/mods/events.md），省的通常比重算少。靜態一次性設定（`skillOverrides: name-only`、`/skill-doctor`）才划算，而且不需要 Jev。
- **用 settings hook 換主模型**：做不到（Q3）。
- **Jev 當主模型**：官方明說不行（https://docs.typesafe.ai/introduction/coding-agents.md）。
- **每回合動態選 CLAUDE.md 規則注入**：CLAUDE.md 在 project context 層、已快取（https://code.claude.com/docs/en/prompt-caching.md），省得很少，還可能漏規則。

### 尚未驗證、下一步建議

1. 寫一個最小的 mod：只做「第 1 名」的 effort 下調＋記錄 `result.usage`，在公開題目上比較 output token 與品質（遵守 D12 的驗證條件：公開資料、有標準答案）。
2. 實測 PreToolUse `updatedInput` 改 Agent 的 `model` 是否生效（看 `resolvedModel`）。
3. 量測使用者真實 session 中 Bash／MCP 輸出超過 4k 字的頻率，判斷第 3 名值不值得做（只看統計，不把工作內容放進 repo）。
4. 量 Jev 對中文 prompt 判 effort／難度的準確度。
