package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rpc is a recorded client session driving the server over JSON-RPC.
type rpc struct {
	t   *testing.T
	in  bytes.Buffer
	nid int
}

func (c *rpc) frame(v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(&c.in, "Content-Length: %d\r\n\r\n%s", len(b), b)
}

func (c *rpc) notify(method string, params any) {
	c.frame(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// call queues a request and returns its id.
func (c *rpc) call(method string, params any) int {
	c.nid++
	c.frame(map[string]any{"jsonrpc": "2.0", "id": c.nid, "method": method, "params": params})
	return c.nid
}

type reply struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Params json.RawMessage `json:"params"`
}

// run feeds the queued session to a fresh server and parses all its output.
func (c *rpc) run() []reply {
	c.t.Helper()
	var out bytes.Buffer
	srv := New(&c.in, &out)
	if err := srv.Run(); err != nil {
		c.t.Fatalf("Run: %v", err)
	}
	var rs []reply
	r := &out
	for r.Len() > 0 {
		var n int
		if _, err := fmt.Fscanf(r, "Content-Length: %d\r\n\r\n", &n); err != nil {
			c.t.Fatalf("bad frame: %v", err)
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			c.t.Fatal(err)
		}
		var rp reply
		if err := json.Unmarshal(body, &rp); err != nil {
			c.t.Fatalf("bad json %s: %v", body, err)
		}
		rs = append(rs, rp)
	}
	return rs
}

func find(rs []reply, id int) *reply {
	for i := range rs {
		if string(rs[i].ID) == fmt.Sprint(id) {
			return &rs[i]
		}
	}
	return nil
}

func diagnosticsFor(rs []reply, uri string) []string {
	var msgs []string
	for _, r := range rs {
		if r.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var p struct {
			URI         string `json:"uri"`
			Diagnostics []struct {
				Message string `json:"message"`
			} `json:"diagnostics"`
		}
		_ = json.Unmarshal(r.Params, &p)
		if p.URI == uri {
			for _, d := range p.Diagnostics {
				msgs = append(msgs, d.Message)
			}
		}
	}
	return msgs
}

// copyProject clones the shared fixture into a temp dir so tests may edit it.
func copyProject(t *testing.T) string {
	t.Helper()
	src := "../../testdata/project"
	dst := t.TempDir()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func uri(p string) string { return "file://" + p }

func newSession(t *testing.T, root string) *rpc {
	c := &rpc{t: t}
	c.call("initialize", map[string]any{"rootUri": uri(root)})
	c.notify("initialized", map[string]any{})
	return c
}

func (c *rpc) open(u, text string) {
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": u, "text": text}})
}

func posOf(text, needle string, delta int) map[string]int {
	i := strings.Index(text, needle) + delta
	line := strings.Count(text[:i], "\n")
	col := i - (strings.LastIndex(text[:i], "\n") + 1)
	return map[string]int{"line": line, "character": col}
}

func at(u string, pos map[string]int) map[string]any {
	return map[string]any{"textDocument": map[string]any{"uri": u}, "position": pos}
}

const badRepo = "package repositories\n\nimport \"github.com/rusmanplatd/basegoravel/app/facades\"\n\nfunc f() {\n\tfacades.DB().Table(\"ref_geo_cities\").Where(\"nope = ?\", 1).Get(nil)\n\t_ = facades.Config().GetString(\"jwt.secret\")\n\t_ = facades.Config().GetString(\"jwt.missing\")\n}\n"

// ---------- positive ----------

func TestServer_Positive_InitializeAdvertisesCapabilities(t *testing.T) {
	root := copyProject(t)
	c := newSession(t, root)
	rs := c.run()
	r := find(rs, 1)
	if r == nil || r.Error != nil {
		t.Fatalf("initialize failed: %+v", r)
	}
	var res struct {
		Capabilities map[string]any `json:"capabilities"`
		ServerInfo   map[string]any `json:"serverInfo"`
	}
	_ = json.Unmarshal(r.Result, &res)
	for _, k := range []string{"hoverProvider", "definitionProvider", "referencesProvider", "renameProvider", "completionProvider", "semanticTokensProvider"} {
		if res.Capabilities[k] == nil {
			t.Errorf("missing capability %s", k)
		}
	}
	if res.ServerInfo["name"] != "goravel-lsp" {
		t.Errorf("serverInfo %v", res.ServerInfo)
	}
}

func TestServer_Positive_DidOpenPublishesDiagnostics(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/bad.go"))
	c := newSession(t, root)
	c.open(u, badRepo)
	msgs := diagnosticsFor(c.run(), u)
	joined := strings.Join(msgs, "|")
	if !strings.Contains(joined, "Column 'nope' does not exist on table 'ref_geo_cities'") {
		t.Errorf("missing column diagnostic: %v", msgs)
	}
	if !strings.Contains(joined, "Unknown config key 'jwt.missing'") {
		t.Errorf("missing config diagnostic: %v", msgs)
	}
	if strings.Contains(joined, "'jwt.secret'") {
		t.Errorf("valid key must not be flagged: %v", msgs)
	}
}

func TestServer_Positive_CleanFilePublishesEmptyDiagnostics(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/ok.go"))
	clean := strings.Replace(badRepo, `.Where("nope = ?", 1)`, `.Where("name = ?", 1)`, 1)
	clean = strings.Replace(clean, "\t_ = facades.Config().GetString(\"jwt.missing\")\n", "", 1)
	c := newSession(t, root)
	c.open(u, clean)
	if msgs := diagnosticsFor(c.run(), u); len(msgs) != 0 {
		t.Errorf("expected clean file, got %v", msgs)
	}
}

func TestServer_Positive_DidChangeRepublishes(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/bad.go"))
	c := newSession(t, root)
	c.open(u, badRepo)
	c.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": u, "version": 2},
		"contentChanges": []map[string]any{{"text": strings.Replace(badRepo, "nope", "name", 1)}},
	})
	var counts []int
	for _, r := range c.run() {
		if r.Method == "textDocument/publishDiagnostics" {
			var p struct{ Diagnostics []any }
			_ = json.Unmarshal(r.Params, &p)
			counts = append(counts, len(p.Diagnostics))
		}
	}
	if len(counts) < 2 || counts[len(counts)-1] >= counts[0] {
		t.Errorf("diagnostics should shrink after the fix, got %v", counts)
	}
}

func TestServer_Positive_HoverOnColumn(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/bad.go"))
	src := strings.Replace(badRepo, "nope", "name", 1)
	c := newSession(t, root)
	c.open(u, src)
	id := c.call("textDocument/hover", at(u, posOf(src, "name = ?", 1)))
	r := find(c.run(), id)
	if r == nil || r.Error != nil || string(r.Result) == "null" || !strings.Contains(string(r.Result), "name") {
		t.Errorf("hover over a valid column should return docs, got %+v", r)
	}
}

func TestServer_Negative_HoverOnUnknownColumnIsNull(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/bad.go"))
	c := newSession(t, root)
	c.open(u, badRepo)
	id := c.call("textDocument/hover", at(u, posOf(badRepo, "nope", 1)))
	r := find(c.run(), id)
	if r == nil || r.Error != nil || string(r.Result) != "null" {
		t.Errorf("unknown column has no docs, got %+v", r)
	}
}

func TestServer_Positive_ReferencesOfConfigKey(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/bad.go"))
	c := newSession(t, root)
	c.open(u, badRepo)
	p := at(u, posOf(badRepo, "jwt.secret", 2))
	p["context"] = map[string]any{"includeDeclaration": true}
	id := c.call("textDocument/references", p)
	r := find(c.run(), id)
	if r == nil || r.Error != nil {
		t.Fatalf("references errored: %+v", r)
	}
	var locs []struct{ URI string }
	_ = json.Unmarshal(r.Result, &locs)
	if len(locs) == 0 {
		t.Error("expected at least one reference to jwt.secret")
	}
}

func TestServer_Positive_PrepareRenameConfigKey(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/bad.go"))
	c := newSession(t, root)
	c.open(u, badRepo)
	id := c.call("textDocument/prepareRename", at(u, posOf(badRepo, "jwt.secret", 2)))
	r := find(c.run(), id)
	var res struct {
		Placeholder string `json:"placeholder"`
	}
	if r != nil {
		_ = json.Unmarshal(r.Result, &res)
	}
	if res.Placeholder != "jwt.secret" {
		t.Errorf("expected placeholder jwt.secret, got %+v", r)
	}
}

func TestServer_Positive_RenameConfigKeyProducesEdits(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/bad.go"))
	c := newSession(t, root)
	c.open(u, badRepo)
	p := at(u, posOf(badRepo, "jwt.secret", 2))
	p["newName"] = "jwt.signing"
	id := c.call("textDocument/rename", p)
	r := find(c.run(), id)
	if r == nil || r.Error != nil {
		t.Fatalf("rename errored: %+v", r)
	}
	if !strings.Contains(string(r.Result), "jwt.signing") {
		t.Errorf("expected edits containing new name, got %s", r.Result)
	}
}

func TestServer_Positive_ShutdownAndExit(t *testing.T) {
	root := copyProject(t)
	c := newSession(t, root)
	id := c.call("shutdown", nil)
	c.notify("exit", nil)
	c.call("textDocument/hover", map[string]any{}) // must never be processed after exit
	rs := c.run()
	if r := find(rs, id); r == nil || r.Error != nil {
		t.Errorf("shutdown should reply, got %+v", r)
	}
	if find(rs, id+1) != nil {
		t.Error("requests after exit must be ignored")
	}
}

// ---------- negative ----------

func TestServer_Negative_UnknownMethodReturnsMethodNotFound(t *testing.T) {
	c := newSession(t, copyProject(t))
	id := c.call("textDocument/doesNotExist", nil)
	r := find(c.run(), id)
	if r == nil || r.Error == nil || r.Error.Code != -32601 {
		t.Errorf("expected -32601, got %+v", r)
	}
}

func TestServer_Negative_UnknownNotificationIsSilent(t *testing.T) {
	c := newSession(t, copyProject(t))
	c.notify("custom/unknown", map[string]any{})
	for _, r := range c.run() {
		if r.Error != nil {
			t.Errorf("notifications must not produce errors: %+v", r)
		}
	}
}

func TestServer_Negative_MalformedJSONSkipped(t *testing.T) {
	c := &rpc{t: t}
	bad := "{not json"
	fmt.Fprintf(&c.in, "Content-Length: %d\r\n\r\n%s", len(bad), bad)
	id := c.call("shutdown", nil)
	if r := find(c.run(), id); r == nil || r.Error != nil {
		t.Errorf("server must survive malformed frame, got %+v", r)
	}
}

func TestServer_Negative_MissingContentLengthEndsRun(t *testing.T) {
	var out bytes.Buffer
	srv := New(strings.NewReader("Foo: bar\r\n\r\n{}"), &out)
	if err := srv.Run(); err == nil {
		t.Error("expected an error for a frame without Content-Length")
	}
}

func TestServer_Negative_EmptyInputReturnsCleanly(t *testing.T) {
	if err := New(strings.NewReader(""), io.Discard).Run(); err != nil {
		t.Errorf("EOF should end Run cleanly, got %v", err)
	}
}

func TestServer_Negative_RequestsOnUnopenedDocument(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/never_opened.go"))
	c := newSession(t, root)
	pos := map[string]int{"line": 0, "character": 0}
	ids := []int{
		c.call("textDocument/hover", at(u, pos)),
		c.call("textDocument/definition", at(u, pos)),
		c.call("textDocument/completion", at(u, pos)),
		c.call("textDocument/references", at(u, pos)),
		c.call("textDocument/prepareRename", at(u, pos)),
	}
	rs := c.run()
	for _, id := range ids {
		if r := find(rs, id); r == nil || r.Error != nil {
			t.Errorf("request %d should reply without error, got %+v", id, r)
		}
	}
}

func TestServer_Negative_RenameWithEmptyNameIsRejected(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/bad.go"))
	c := newSession(t, root)
	c.open(u, badRepo)
	p := at(u, posOf(badRepo, "jwt.secret", 2))
	p["newName"] = "   "
	id := c.call("textDocument/rename", p)
	r := find(c.run(), id)
	if r == nil || string(r.Result) != "null" {
		t.Errorf("blank newName must yield null, got %+v", r)
	}
}

func TestServer_Negative_PrepareRenameOffSymbol(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/bad.go"))
	c := newSession(t, root)
	c.open(u, badRepo)
	id := c.call("textDocument/prepareRename", at(u, map[string]int{"line": 0, "character": 2}))
	r := find(c.run(), id)
	if r == nil || string(r.Result) != "null" {
		t.Errorf("package keyword is not renameable, got %+v", r)
	}
}

func TestServer_Negative_HoverOutsideAnySpanIsNull(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/bad.go"))
	c := newSession(t, root)
	c.open(u, badRepo)
	id := c.call("textDocument/hover", at(u, map[string]int{"line": 0, "character": 2}))
	r := find(c.run(), id)
	if r == nil || r.Error != nil || string(r.Result) != "null" {
		t.Errorf("expected null hover, got %+v", r)
	}
}

func TestServer_Negative_NoWorkspaceRootDoesNotPanic(t *testing.T) {
	c := &rpc{t: t}
	c.call("initialize", map[string]any{})
	u := "file:///nowhere/a.go"
	c.open(u, badRepo)
	c.call("textDocument/hover", at(u, map[string]int{"line": 5, "character": 5}))
	_ = c.run()
}

func TestServer_Negative_CloseDocumentThenRequest(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/bad.go"))
	c := newSession(t, root)
	c.open(u, badRepo)
	c.notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": u}})
	id := c.call("textDocument/hover", at(u, posOf(badRepo, "nope", 1)))
	if r := find(c.run(), id); r == nil || r.Error != nil {
		t.Errorf("hover after close must not error, got %+v", r)
	}
}

func TestServer_Positive_HoverOnTableName(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/bad.go"))
	c := newSession(t, root)
	c.open(u, badRepo)
	id := c.call("textDocument/hover", at(u, posOf(badRepo, "ref_geo_cities", 2)))
	r := find(c.run(), id)
	if r == nil || r.Error != nil {
		t.Fatalf("hover errored: %+v", r)
	}
	var res struct {
		Contents struct{ Kind, Value string }
		Range    Range
	}
	_ = json.Unmarshal(r.Result, &res)
	if res.Contents.Kind != "markdown" || !strings.Contains(res.Contents.Value, "**Table `ref_geo_cities`**") {
		t.Errorf("unexpected hover payload: %s", r.Result)
	}
	if res.Range.End.Character-res.Range.Start.Character != len("ref_geo_cities") {
		t.Errorf("range should span the table name, got %+v", res.Range)
	}
}

func TestServer_Negative_HoverOnUnknownTableIsNull(t *testing.T) {
	root := copyProject(t)
	u := uri(filepath.Join(root, "app/repositories/bad.go"))
	src := strings.Replace(badRepo, "ref_geo_cities", "ghost_table", 1)
	c := newSession(t, root)
	c.open(u, src)
	id := c.call("textDocument/hover", at(u, posOf(src, "ghost_table", 2)))
	if r := find(c.run(), id); r == nil || r.Error != nil || string(r.Result) != "null" {
		t.Errorf("expected null, got %+v", r)
	}
}
