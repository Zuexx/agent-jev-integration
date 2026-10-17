// Package blendermcp is a small stdio MCP server that drives a headless
// Blender, for the bench's Blender tasks (docs/eval-plan.md 3.4).
//
// Every tool call starts `blender -b`, loads the scene file from the working
// directory (an empty scene if there is none yet), does its work and saves
// the scene, so state carries over between calls without a Blender window.
// Rendering is always Cycles on the CPU with capped samples: the same scene
// gives the same image on any machine, only the time differs.
package blendermcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// MaxSamples caps render samples so a run cannot spend minutes per frame.
const MaxSamples = 128

// Runner runs one Blender Python script against the scene; Exec is the real one.
type Runner interface {
	Run(ctx context.Context, script string, args map[string]any) (string, error)
}

// Exec runs the Blender binary at Path with Scene as the scene file.
type Exec struct {
	Path, Scene string
	Timeout     time.Duration
}

// The begin/end markers separate the tool's output from Blender's own log.
const (
	begin = "<<<JEVCC-BLENDER-OUT>>>"
	end   = "<<<JEVCC-BLENDER-END>>>"
)

// prelude loads or creates the scene and, after the tool body ran, saves it.
const prelude = `
import bpy, json, os, sys, io, contextlib, traceback
ARGS = json.loads(os.environ["JEVCC_BLENDER_ARGS"])
SCENE = os.environ["JEVCC_BLENDER_SCENE"]
if os.path.exists(SCENE):
    bpy.ops.wm.open_mainfile(filepath=SCENE)
else:
    bpy.ops.wm.read_factory_settings(use_empty=True)
_out = io.StringIO()
_ok = True
with contextlib.redirect_stdout(_out):
    try:
        BODY()
    except Exception:
        traceback.print_exc(file=_out)
        _ok = False
if _ok:
    bpy.ops.wm.save_as_mainfile(filepath=SCENE)
sys.__stdout__.write("` + begin + `\n" + ("" if _ok else "ERROR\n") + _out.getvalue() + "\n` + end + `\n")
sys.__stdout__.flush()
`

func (e Exec) Run(ctx context.Context, body string, args map[string]any) (string, error) {
	dir, err := os.MkdirTemp("", "jevcc-blender-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	script := filepath.Join(dir, "tool.py")
	// The body is a function so the prelude can wrap it in error handling.
	src := "def BODY():\n" + indent(body) + "\n" + prelude
	if err := os.WriteFile(script, []byte(src), 0o600); err != nil {
		return "", err
	}
	a, _ := json.Marshal(args)
	scene, _ := filepath.Abs(e.Scene)
	timeout := e.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.Path, "-b", "--factory-startup", "--python", script)
	cmd.Dir = filepath.Dir(scene) // relative render paths are project paths
	cmd.Env = append(os.Environ(), "JEVCC_BLENDER_ARGS="+string(a), "JEVCC_BLENDER_SCENE="+scene)
	out, err := cmd.CombinedOutput()
	s := string(out)
	i, j := strings.Index(s, begin), strings.LastIndex(s, end)
	if i < 0 || j < i {
		return "", fmt.Errorf("blender failed: %v: %s", err, tail(s, 800))
	}
	res := strings.TrimSpace(s[i+len(begin) : j])
	if rest, ok := strings.CutPrefix(res, "ERROR\n"); ok {
		return "", fmt.Errorf("%s", rest)
	}
	return res, nil
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return strings.Join(lines, "\n") + "\n    pass"
}

func tail(s string, n int) string {
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}

// Tool bodies. They run inside BODY() with ARGS bound to the call arguments.
const (
	runPythonBody = `exec(compile(ARGS["code"], "<tool>", "exec"), {"bpy": bpy, "__name__": "__main__"})`

	sceneInfoBody = `
sc = bpy.context.scene
objs = []
for o in sc.objects:
    d = {"name": o.name, "type": o.type, "location": [round(v, 3) for v in o.location],
         "rotation_euler": [round(v, 3) for v in o.rotation_euler], "dimensions": [round(v, 3) for v in o.dimensions],
         "hide_render": o.hide_render, "materials": [s.material.name for s in o.material_slots if s.material]}
    if o.type == "LIGHT":
        d["light"] = {"type": o.data.type, "energy": o.data.energy}
    if o.type == "CAMERA":
        d["camera"] = {"lens_mm": round(o.data.lens, 2)}
    objs.append(d)
print(json.dumps({"objects": objs, "camera": sc.camera.name if sc.camera else None,
    "resolution": [sc.render.resolution_x, sc.render.resolution_y, sc.render.resolution_percentage],
    "engine": sc.render.engine, "world": bool(sc.world)}, indent=1))`

	renderBody = `
import time
sc = bpy.context.scene
if sc.camera is None:
    raise RuntimeError("the scene has no active camera (scene.camera)")
sc.render.engine = "CYCLES"
sc.cycles.device = "CPU"
sc.cycles.samples = int(ARGS["samples"])
out = os.path.abspath(ARGS["output"])
os.makedirs(os.path.dirname(out), exist_ok=True)
sc.render.filepath = out
sc.render.image_settings.file_format = "PNG"
t = time.time()
bpy.ops.render.render(write_still=True)
print(json.dumps({"output": ARGS["output"], "seconds": round(time.time() - t, 1),
    "resolution": [sc.render.resolution_x * sc.render.resolution_percentage // 100,
                   sc.render.resolution_y * sc.render.resolution_percentage // 100], "samples": sc.cycles.samples}))`
)

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

var tools = []tool{
	{"run_python", "Run Python (bpy) against the current Blender scene; the scene is saved afterwards. print() output is returned. " +
		"The scene starts empty (no default cube, camera or light).",
		schema(map[string]any{"code": map[string]any{"type": "string", "description": "Python code; bpy is imported"}}, "code")},
	{"scene_info", "Summarise the current scene: objects with transforms and materials, lights, camera, resolution.",
		schema(map[string]any{})},
	{"render", fmt.Sprintf("Render the active camera to a PNG with Cycles on the CPU (samples at most %d). Uses the scene's resolution.", MaxSamples),
		schema(map[string]any{
			"output":  map[string]any{"type": "string", "description": "PNG path relative to the project, e.g. renders/shot.png"},
			"samples": map[string]any{"type": "integer", "description": fmt.Sprintf("1-%d, default 64", MaxSamples)},
		}, "output")},
}

func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Serve answers MCP requests on in/out (one JSON-RPC message per line) until in closes.
func Serve(ctx context.Context, in io.Reader, out io.Writer, r Runner) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			enc.Encode(map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32700, "message": "parse error"}})
			continue
		}
		if len(req.ID) == 0 {
			continue // a notification, e.g. notifications/initialized
		}
		result, rpcErr := handle(ctx, req, r)
		msg := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			msg["error"] = rpcErr
		} else {
			msg["result"] = result
		}
		if err := enc.Encode(msg); err != nil {
			return err
		}
	}
	return sc.Err()
}

func handle(ctx context.Context, req request, r Runner) (any, map[string]any) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		return map[string]any{"protocolVersion": p.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{"name": "jevcc-blender", "version": "1"}}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, map[string]any{"code": -32602, "message": err.Error()}
		}
		text, err := call(ctx, r, p.Name, p.Arguments)
		if err != nil {
			return map[string]any{"content": []any{map[string]any{"type": "text", "text": err.Error()}}, "isError": true}, nil
		}
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}, nil
	}
	return nil, map[string]any{"code": -32601, "message": "method not found: " + req.Method}
}

func call(ctx context.Context, r Runner, name string, args map[string]any) (string, error) {
	if args == nil {
		args = map[string]any{}
	}
	switch name {
	case "run_python":
		code, _ := args["code"].(string)
		if strings.TrimSpace(code) == "" {
			return "", fmt.Errorf("code is required")
		}
		out, err := r.Run(ctx, runPythonBody, map[string]any{"code": code})
		if err == nil && out == "" {
			out = "(ok, no output)"
		}
		return out, err
	case "scene_info":
		return r.Run(ctx, sceneInfoBody, nil)
	case "render":
		output, _ := args["output"].(string)
		if output == "" || filepath.IsAbs(output) || strings.HasPrefix(output, "/") || strings.HasPrefix(output, `\`) || filepath.VolumeName(output) != "" || strings.Contains(output, "..") || !strings.HasSuffix(strings.ToLower(output), ".png") {
			return "", fmt.Errorf("output must be a relative .png path inside the project")
		}
		samples := 64
		if f, ok := args["samples"].(float64); ok {
			samples = int(f)
		}
		samples = max(1, min(samples, MaxSamples))
		return r.Run(ctx, renderBody, map[string]any{"output": output, "samples": samples})
	}
	return "", fmt.Errorf("unknown tool %q", name)
}
