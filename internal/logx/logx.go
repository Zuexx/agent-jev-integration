// Package logx appends diagnostics to ~/.jevcc/log. It never fails loudly:
// the hook must not break claude because logging failed.
package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zuexx/agent-jev-integration/internal/paths"
)

const maxSize = 1 << 20

func Path() string { return filepath.Join(paths.StateDir(), "log") }

func Printf(format string, args ...any) {
	p := Path()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if fi, err := os.Stat(p); err == nil && fi.Size() > maxSize {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}
