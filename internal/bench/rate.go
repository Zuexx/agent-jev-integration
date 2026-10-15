package bench

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The human rating page (docs/eval-plan.md 5.3): a local HTML file showing a
// blinded, shuffled sample of outputs with each task's five score options.
// The key from item id to run is written to a separate file the page never
// reads, so the rater cannot tell which arm made what.

// HumanScale is a task's human.json.
type HumanScale struct {
	Question string `json:"question"`
	Options  []struct {
		Score int    `json:"score"`
		Label string `json:"label"`
	} `json:"options"`
}

// RateItem is one output to rate.
type RateItem struct {
	ID     string
	Task   string
	Prompt string
	Scale  HumanScale
	Files  []RateFile
}

// RateFile is one artifact shown on the page.
type RateFile struct {
	Name, Kind, Src, Text string // Kind: text, image, video
	Markdown              bool   // render as formatted text
}

// RateKeyEntry maps a blinded id back to its run.
type RateKeyEntry struct {
	ID   string `json:"id"`
	Task string `json:"task"`
	Arm  string `json:"arm"`
	Rep  int    `json:"rep"`
	Run  string `json:"artifacts"`
}

// SampleForRating picks up to perTask runs per task that has human.json,
// spread across arms, in a seeded random order.
func SampleForRating(runs []Run, tasks map[string]Task, perTask int, seed int64) []Run {
	rng := rand.New(rand.NewSource(seed))
	byTask := map[string]map[string][]Run{}
	for _, r := range runs {
		t, ok := tasks[r.Task]
		if !ok || r.Error != "" || r.Artifacts == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(t.Dir, "human.json")); err != nil {
			continue
		}
		if byTask[r.Task] == nil {
			byTask[r.Task] = map[string][]Run{}
		}
		byTask[r.Task][r.Arm] = append(byTask[r.Task][r.Arm], r)
	}
	var out []Run
	names := make([]string, 0, len(byTask))
	for n := range byTask {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		arms := make([]string, 0, len(byTask[n]))
		for a := range byTask[n] {
			arms = append(arms, a)
		}
		sort.Strings(arms)
		for _, a := range arms {
			rs := byTask[n][a]
			rng.Shuffle(len(rs), func(i, j int) { rs[i], rs[j] = rs[j], rs[i] })
		}
		// Round-robin over arms so every arm is represented.
		for i, picked := 0, 0; picked < perTask; i++ {
			progress := false
			for _, a := range arms {
				if i < len(byTask[n][a]) && picked < perTask {
					out = append(out, byTask[n][a][i])
					picked++
					progress = true
				}
			}
			if !progress {
				break
			}
		}
	}
	rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// WriteRatingPage writes rate.html and rate-key.json into dir (the results
// directory, so artifact paths stay relative) for the sampled runs.
func WriteRatingPage(dir, streams string, sample []Run, tasks map[string]Task) error {
	var items []RateItem
	var key []RateKeyEntry
	for i, r := range sample {
		t := tasks[r.Task]
		var scale HumanScale
		b, err := os.ReadFile(filepath.Join(t.Dir, "human.json"))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &scale); err != nil {
			return fmt.Errorf("%s/human.json: %w", t.Name, err)
		}
		id := fmt.Sprintf("r%02d", i+1)
		it := RateItem{ID: id, Task: r.Task, Prompt: t.Prompt, Scale: scale}
		root := filepath.Join(streams, r.Artifacts)
		// Media is copied under the blinded id: artifact paths name the arm.
		media := func(p, rel string) string {
			dst := filepath.Join(dir, "rate-media", id, rel)
			os.MkdirAll(filepath.Dir(dst), 0o755)
			if b, err := os.ReadFile(p); err == nil {
				os.WriteFile(dst, b, 0o644)
			}
			return filepath.ToSlash(filepath.Join("rate-media", id, rel))
		}
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			switch ext := strings.ToLower(filepath.Ext(p)); ext {
			case ".png", ".jpg", ".jpeg", ".webp":
				it.Files = append(it.Files, RateFile{Name: rel, Kind: "image", Src: media(p, rel)})
			case ".mp4", ".webm", ".mov":
				it.Files = append(it.Files, RateFile{Name: rel, Kind: "video", Src: media(p, rel)})
			case ".md", ".txt", ".csv", ".json", ".go", ".ts", ".tsx", ".py", ".yaml", ".yml":
				if b, err := os.ReadFile(p); err == nil && len(b) < 200_000 {
					it.Files = append(it.Files, RateFile{Name: rel, Kind: "text", Text: string(b), Markdown: ext == ".md"})
				}
			}
			return nil
		})
		// Show what matters first: media, then documents, then code.
		sort.SliceStable(it.Files, func(a, b int) bool { return rank(it.Files[a]) < rank(it.Files[b]) })
		items = append(items, it)
		key = append(key, RateKeyEntry{ID: id, Task: r.Task, Arm: r.Arm, Rep: r.Rep, Run: r.Artifacts})
	}
	kb, _ := json.MarshalIndent(key, "", " ")
	if err := os.WriteFile(filepath.Join(dir, "rate-key.json"), kb, 0o644); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "rate.html"))
	if err != nil {
		return err
	}
	defer f.Close()
	return ratePage.Execute(f, items)
}

func rank(f RateFile) int {
	switch {
	case f.Kind == "video" || f.Kind == "image":
		return 0
	case strings.HasSuffix(f.Name, ".md"):
		return 1
	}
	return 2
}

var ratePage = template.Must(template.New("rate").Parse(`<!doctype html>
<html lang="zh-Hant"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Blind Output Rating</title>
<style>
:root{--bg:#f7f6f2;--card:#fff;--ink:#1d2329;--muted:#5f6b76;--line:#dcd9d0;--accent:#1f5fa8;--pick:#e7f0fb}
@media (prefers-color-scheme:dark){:root:not([data-theme="light"]){--bg:#15181b;--card:#1e2226;--ink:#e8eaed;--muted:#9aa4ad;--line:#33393f;--accent:#7fb2ee;--pick:#1f3047}}
:root[data-theme="dark"]{--bg:#15181b;--card:#1e2226;--ink:#e8eaed;--muted:#9aa4ad;--line:#33393f;--accent:#7fb2ee;--pick:#1f3047}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:16px/1.55 -apple-system,"PingFang TC","Noto Sans TC",system-ui,sans-serif}
header{position:sticky;top:0;z-index:2;background:var(--bg);border-bottom:1px solid var(--line);padding:12px 16px;display:flex;gap:12px;align-items:center;flex-wrap:wrap}
header h1{font-size:18px;margin:0;flex:1}#progress{color:var(--muted)}
button{font:inherit;padding:8px 14px;border:1px solid var(--accent);background:var(--accent);color:#fff;border-radius:8px;cursor:pointer}
main{max-width:980px;margin:0 auto;padding:16px}
.item{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:16px;margin:0 0 20px}
.item h2{font-size:16px;margin:0 0 4px}.q{font-weight:600;margin:12px 0 8px}
details{margin:6px 0;color:var(--muted)}details pre{color:var(--ink)}
pre{white-space:pre-wrap;word-break:break-word;background:var(--bg);border:1px solid var(--line);border-radius:8px;padding:12px;max-height:520px;overflow:auto;font:13px/1.5 ui-monospace,Menlo,monospace}
img,video{max-width:100%;border-radius:8px;border:1px solid var(--line);display:block;margin:8px 0}
.fname{font:12px ui-monospace,Menlo,monospace;color:var(--muted);margin-top:10px}
label.opt{display:flex;gap:10px;align-items:flex-start;padding:8px 10px;border:1px solid var(--line);border-radius:8px;margin:6px 0;cursor:pointer}
label.opt:has(input:checked){background:var(--pick);border-color:var(--accent)}
.md{border:1px solid var(--line);border-radius:8px;padding:4px 16px;max-height:640px;overflow:auto;background:var(--card)}
.md table{border-collapse:collapse;margin:8px 0;font-size:14px}.md th,.md td{border:1px solid var(--line);padding:4px 8px;text-align:left}
.md code{font:13px ui-monospace,Menlo,monospace;background:var(--bg);padding:1px 4px;border-radius:4px}.md blockquote{margin:8px 0;padding-left:12px;border-left:3px solid var(--line);color:var(--muted)}
.md h1{font-size:20px}.md h2{font-size:18px}.md h3{font-size:16px}
textarea{width:100%;min-height:56px;font:inherit;background:var(--bg);color:var(--ink);border:1px solid var(--line);border-radius:8px;padding:8px}
</style></head><body>
<header><h1>盲評：成品評分</h1><span id="progress"></span><button id="export">匯出 ratings.json</button></header>
<main>
<p>每一份成品的來源都隱藏了，順序也是隨機的。請依你的感受，為每一份挑一個最接近的分數；可以分多次評，進度會留在這個瀏覽器裡。全部評完後按右上角「匯出」，把下載的 ratings.json 交回。</p>
{{range .}}<section class="item" data-id="{{.ID}}">
<h2>{{.ID}}</h2>
<details><summary>當時給的題目（英文原文）</summary><pre>{{.Prompt}}</pre></details>
{{range .Files}}<div class="fname">{{.Name}}</div>{{if eq .Kind "image"}}<img loading="lazy" src="{{.Src}}" alt="{{.Name}}">{{else if eq .Kind "video"}}<video controls preload="metadata" src="{{.Src}}"></video>{{else if .Markdown}}<div class="md" data-text="{{.Text}}"></div><details><summary>原始文字</summary><pre>{{.Text}}</pre></details>{{else}}<pre>{{.Text}}</pre>{{end}}{{end}}
<div class="q">{{.Scale.Question}}</div>
{{$id := .ID}}{{range .Scale.Options}}<label class="opt"><input type="radio" name="{{$id}}" value="{{.Score}}"><span><b>{{.Score}}</b>　{{.Label}}</span></label>{{end}}
<textarea placeholder="備註（選填）" data-note="{{.ID}}"></textarea>
</section>{{end}}
</main>
<script>
// A small offline Markdown renderer: headings, lists, tables, quotes, code
// blocks, bold, italics and inline code. Text is escaped first.
const esc = s => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
const inline = s => esc(s).replace(/\x60([^\x60]+)\x60/g, "<code>$1</code>").replace(/\*\*([^*]+)\*\*/g, "<b>$1</b>").replace(/(^|[^*])\*([^*\s][^*]*)\*/g, "$1<i>$2</i>");
function md(src) {
  const lines = src.split("\n"), out = [];
  for (let i = 0; i < lines.length; i++) {
    const l = lines[i];
    if (/^\x60\x60\x60/.test(l)) { const b = []; while (++i < lines.length && !/^\x60\x60\x60/.test(lines[i])) b.push(lines[i]); out.push("<pre>" + esc(b.join("\n")) + "</pre>"); continue; }
    let m;
    if ((m = l.match(/^(#{1,6})\s+(.*)$/))) { out.push("<h" + Math.min(m[1].length + 0, 6) + ">" + inline(m[2]) + "</h" + Math.min(m[1].length, 6) + ">"); continue; }
    if (/^\s*\|.*\|\s*$/.test(l) && i + 1 < lines.length && /^\s*\|?[\s:|-]+\|?\s*$/.test(lines[i + 1])) {
      const cells = r => r.trim().replace(/^\||\|$/g, "").split("|").map(c => inline(c.trim()));
      let t = "<table><tr>" + cells(l).map(c => "<th>" + c + "</th>").join("") + "</tr>"; i++;
      while (i + 1 < lines.length && /^\s*\|/.test(lines[i + 1])) { i++; t += "<tr>" + cells(lines[i]).map(c => "<td>" + c + "</td>").join("") + "</tr>"; }
      out.push(t + "</table>"); continue;
    }
    if ((m = l.match(/^\s*([-*]|\d+\.)\s+(.*)$/))) {
      const tag = /\d/.test(m[1]) ? "ol" : "ul"; let t = "<" + tag + "><li>" + inline(m[2]) + "</li>";
      while (i + 1 < lines.length && (m = lines[i + 1].match(/^\s*([-*]|\d+\.)\s+(.*)$/))) { i++; t += "<li>" + inline(m[2]) + "</li>"; }
      out.push(t + "</" + tag + ">"); continue;
    }
    if ((m = l.match(/^>\s?(.*)$/))) { out.push("<blockquote>" + inline(m[1]) + "</blockquote>"); continue; }
    if (l.trim()) out.push("<p>" + inline(l) + "</p>");
  }
  return out.join("\n");
}
document.querySelectorAll(".md").forEach(el => { el.innerHTML = md(el.dataset.text); });
const KEY = "jevcc-rate:" + location.pathname;
let state = {};
try { state = JSON.parse(localStorage.getItem(KEY) || "{}"); } catch (e) {}
const save = () => { try { localStorage.setItem(KEY, JSON.stringify(state)); } catch (e) {} };
const items = [...document.querySelectorAll(".item")];
const progress = () => { document.getElementById("progress").textContent = Object.values(state).filter(v => v.score).length + " / " + items.length + " 已評"; };
items.forEach(el => {
  const id = el.dataset.id, s = state[id] || {};
  if (s.score) { const r = el.querySelector('input[value="' + s.score + '"]'); if (r) r.checked = true; }
  const note = el.querySelector("textarea"); note.value = s.note || "";
  el.addEventListener("change", e => { if (e.target.name === id) { state[id] = Object.assign(state[id] || {}, {score: +e.target.value}); save(); progress(); } });
  note.addEventListener("input", () => { state[id] = Object.assign(state[id] || {}, {note: note.value}); save(); });
});
progress();
document.getElementById("export").onclick = () => {
  const blob = new Blob([JSON.stringify(state, null, 1)], {type: "application/json"});
  const a = document.createElement("a"); a.href = URL.createObjectURL(blob); a.download = "ratings.json"; a.click();
};
</script></body></html>
`))
