// Package journal keeps the JSONL logs in the state directory as one file per
// day (decisions-2026-10-14.jsonl) and removes days past the retention. A
// single file grows without limit and cannot be read by time range; a file per
// day can be, and old days can be deleted whole.
package journal

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/fsutil"
	"github.com/zuexx/agent-jev-integration/internal/paths"
)

const dayLayout = "2006-01-02"

// Path is the file of kind (e.g. "decisions") that holds records for day.
func Path(kind string, day time.Time) string {
	return filepath.Join(paths.StateDir(), kind+"-"+day.Format(dayLayout)+".jsonl")
}

// Append writes v as one line to today's file of kind. The first write of a
// day also removes the files of kind older than keepDays.
func Append(kind string, v any, keepDays int, now time.Time) error {
	p := Path(kind, now)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		prune(kind, keepDays, now)
	}
	return fsutil.AppendJSONL(p, v)
}

// Files lists the files of kind, oldest first. The single file that older
// versions wrote (kind.jsonl) comes first if it exists; it is never pruned.
func Files(kind string) []string { return FilesIn(paths.StateDir(), kind) }

// FilesIn is Files for another state directory (an isolated bench run's).
func FilesIn(dir, kind string) []string {
	var out []string
	legacy := filepath.Join(dir, kind+".jsonl")
	if _, err := os.Stat(legacy); err == nil {
		out = append(out, legacy)
	}
	daily := dailyFiles(dir, kind)
	return append(out, daily...)
}

// dailyFiles returns the dated files of kind, oldest first. The date layout
// sorts lexically, so no parsing is needed for the order.
func dailyFiles(dir, kind string) []string {
	matches, _ := filepath.Glob(filepath.Join(dir, kind+"-*.jsonl"))
	sort.Strings(matches)
	return matches
}

// prune removes dated files of kind that fall before the retention window.
// Days are compared as calendar dates in local time: a day is kept for
// keepDays days counting today.
func prune(kind string, keepDays int, now time.Time) {
	if keepDays < 1 {
		return
	}
	oldest := now.AddDate(0, 0, -(keepDays - 1)).Format(dayLayout)
	prefix := kind + "-"
	for _, f := range dailyFiles(paths.StateDir(), kind) {
		name := filepath.Base(f)
		day := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".jsonl")
		if day < oldest {
			_ = os.Remove(f)
		}
	}
}
