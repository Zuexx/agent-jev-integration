package blendermcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct{ calls []map[string]any }

func (f *fakeRunner) Run(_ context.Context, body string, args map[string]any) (string, error) {
	f.calls = append(f.calls, args)
	return "ran", nil
}

func serve(t *testing.T, r Runner, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, r); err != nil {
		t.Fatal(err)
	}
	var msgs []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatal(l, err)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func TestProtocol(t *testing.T) {
	f := &fakeRunner{}
	msgs := serve(t, f,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"render","arguments":{"output":"renders/a.png","samples":4096}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"render","arguments":{"output":"/etc/x.png"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"nope"}`,
	)
	if len(msgs) != 5 {
		t.Fatalf("notification must not be answered: %v", msgs)
	}
	if v := msgs[0]["result"].(map[string]any)["protocolVersion"]; v != "2025-06-18" {
		t.Fatal(v)
	}
	if n := len(msgs[1]["result"].(map[string]any)["tools"].([]any)); n != 3 {
		t.Fatal(n)
	}
	if s := f.calls[0]["samples"]; s != MaxSamples {
		t.Fatal("samples not capped:", s)
	}
	if msgs[3]["result"].(map[string]any)["isError"] != true {
		t.Fatal("absolute output path accepted", msgs[3])
	}
	if msgs[4]["error"] == nil {
		t.Fatal(msgs[4])
	}
}

// TestRealBlender drives the real binary when BLENDER points at one.
func TestRealBlender(t *testing.T) {
	bin := os.Getenv("BLENDER")
	if bin == "" {
		t.Skip("BLENDER not set")
	}
	dir := t.TempDir()
	e := Exec{Path: bin, Scene: filepath.Join(dir, "scene.blend"), Timeout: 3 * time.Minute}
	ctx := context.Background()
	if _, err := call(ctx, e, "run_python", map[string]any{"code": "bpy.ops.mesh.primitive_cube_add()\nbpy.ops.object.camera_add(location=(6,-6,4), rotation=(1.1,0,0.8))\nbpy.context.scene.camera = bpy.context.object\nbpy.context.scene.render.resolution_x, bpy.context.scene.render.resolution_y = 64, 36\nprint('made')"}); err != nil {
		t.Fatal(err)
	}
	info, err := call(ctx, e, "scene_info", nil)
	if err != nil || !strings.Contains(info, `"camera": "Camera"`) || !strings.Contains(info, "Cube") {
		t.Fatal(info, err)
	}
	if _, err := call(ctx, e, "run_python", map[string]any{"code": "raise ValueError('boom')"}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatal("python error not reported:", err)
	}
	out, err := call(ctx, e, "render", map[string]any{"output": "renders/t.png", "samples": 4.0})
	if err != nil || !strings.Contains(out, `"resolution": [64, 36]`) {
		t.Fatal(out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "renders", "t.png")); err != nil {
		t.Fatal(err)
	}
}
