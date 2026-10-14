package review

import (
	"context"
	"strings"
	"testing"

	"github.com/zuexx/agent-jev-integration/internal/jev"
)

const diff = `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -10,3 +10,4 @@ func x() {
 	a := 1
+	log.Printf("token=%s", tok)
 	b := 2
@@ -40,2 +41,2 @@ func y() {
-	return err
+	return nil
diff --git a/gone.go b/gone.go
deleted file mode 100644
--- a/gone.go
+++ /dev/null
@@ -1,1 +0,0 @@
-package gone
`

func TestHunks(t *testing.T) {
	hs := Hunks(diff)
	if len(hs) != 2 || hs[0].File != "a.go" || hs[0].Line != 10 || hs[1].Line != 41 || !strings.Contains(hs[0].Text, "token=") {
		t.Fatalf("%+v", hs)
	}
}

type fake struct{}

func (fake) Ask(_ context.Context, state any, qs map[string]jev.Question) (*jev.Response, error) {
	h := state.(map[string]any)["hunk"].(string)
	ans := map[string]jev.Answer{}
	for id := range qs {
		ans[id] = jev.Answer{Type: "noul", Noul: 0.05}
	}
	if strings.Contains(h, "token=") {
		ans["sensitive_logged"] = jev.Answer{Type: "noul", Noul: 0.9}
		ans["debug_left"] = jev.Answer{Type: "noul", Noul: 0.95}
	}
	if strings.Contains(h, "return nil") {
		ans["error_swallowed"] = jev.Answer{Type: "noul", Noul: 0.7}
	}
	return &jev.Response{Answers: ans, Usage: jev.Usage{InputTokens: 2000}}, nil
}

func TestRunRanksBySeverity(t *testing.T) {
	rep := Run(context.Background(), fake{}, Hunks(diff), DefaultChecklist, 0.5, 4)
	if rep.Hunks != 2 || rep.Questions != 2*len(DefaultChecklist) || len(rep.Findings) != 3 || rep.Tokens != 4000 {
		t.Fatalf("%+v", rep)
	}
	// high 0.9 (2.7) > high 0.7 (2.1) > low 0.95 (0.95)
	got := []string{rep.Findings[0].Item.ID, rep.Findings[1].Item.ID, rep.Findings[2].Item.ID}
	if strings.Join(got, ",") != "sensitive_logged,error_swallowed,debug_left" {
		t.Fatal(got)
	}
	md := rep.Markdown(2)
	if !strings.Contains(md, "`a.go:10`") || !strings.Contains(md, "1 lower-priority") {
		t.Fatal(md)
	}
}

func TestChecklistIDsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, it := range DefaultChecklist {
		if seen[it.ID] || weight[it.Severity] == 0 {
			t.Fatalf("%+v", it)
		}
		seen[it.ID] = true
	}
}
