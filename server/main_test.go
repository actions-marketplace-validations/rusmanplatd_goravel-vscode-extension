package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

func writeProjectFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func makeLSPFrame(v any) string {
	b, _ := json.Marshal(v)
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(b), b)
}

func TestParseSQLSourceFlag(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		idx       int
		wantSrc   indexer.SQLSource
		wantNext  int
		wantOK    bool
		wantErr   string
	}{
		{
			name:     "live-db shorthand",
			args:     []string{"--live-db", "."},
			idx:      0,
			wantSrc:  indexer.SQLSourceLiveDB,
			wantNext: 0,
			wantOK:   true,
		},
		{
			name:     "separate sql-source model",
			args:     []string{"--sql-source", "model", "."},
			idx:      0,
			wantSrc:  indexer.SQLSourceModel,
			wantNext: 1,
			wantOK:   true,
		},
		{
			name:     "inline sql-source both",
			args:     []string{"--sql-source=both"},
			idx:      0,
			wantSrc:  indexer.SQLSourceBoth,
			wantNext: 0,
			wantOK:   true,
		},
		{
			name:     "inline sql-source auto",
			args:     []string{"--sql-source=auto"},
			idx:      0,
			wantSrc:  indexer.SQLSourceAuto,
			wantNext: 0,
			wantOK:   true,
		},
		{
			name:     "missing value after --sql-source",
			args:     []string{"--sql-source"},
			idx:      0,
			wantNext: 0,
			wantOK:   true,
			wantErr:  "requires a value",
		},
		{
			name:     "invalid separate value",
			args:     []string{"--sql-source", "postgres"},
			idx:      0,
			wantNext: 1,
			wantOK:   true,
			wantErr:  `invalid --sql-source "postgres"`,
		},
		{
			name:     "invalid inline value",
			args:     []string{"--sql-source=invalid"},
			idx:      0,
			wantNext: 0,
			wantOK:   true,
			wantErr:  `invalid --sql-source "invalid"`,
		},
		{
			name:     "unrelated flag",
			args:     []string{"--errors-only"},
			idx:      0,
			wantNext: 0,
			wantOK:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, next, handled, err := parseSQLSourceFlag(tc.args, tc.idx)
			if handled != tc.wantOK {
				t.Fatalf("handled=%v want %v", handled, tc.wantOK)
			}
			if next != tc.wantNext {
				t.Errorf("nextIdx=%d want %d", next, tc.wantNext)
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err=%v want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if src != tc.wantSrc {
				t.Errorf("src=%q want %q", src, tc.wantSrc)
			}
		})
	}
}

func TestRunCLI_VersionAliases(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		t.Run(arg, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			code := runCLI([]string{arg}, strings.NewReader(""), &out, &errBuf)
			if code != 0 {
				t.Fatalf("exit=%d want 0 (stderr=%s)", code, errBuf.String())
			}
			want := fmt.Sprintf("goravel-lsp version %s\n", version)
			if out.String() != want {
				t.Errorf("stdout=%q want %q", out.String(), want)
			}
		})
	}
}

func TestRunCLI_Dump(t *testing.T) {
	for _, cmd := range []string{"dump", "--dump"} {
		t.Run(cmd, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			code := runCLI([]string{cmd, "testdata/project"}, strings.NewReader(""), &out, &errBuf)
			if code != 0 {
				t.Fatalf("exit=%d want 0", code)
			}
			got := out.String()
			for _, want := range []string{"ref_geo_cities (RefGeoCity)", "ref_geo_provinces (RefGeoProvince)", "Summary:"} {
				if !strings.Contains(got, want) {
					t.Errorf("dump output missing %q:\n%s", want, got)
				}
			}
		})
	}

	t.Run("dump default root", func(t *testing.T) {
		origWD, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		tmp := t.TempDir()
		if err := os.Chdir(tmp); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origWD) })

		var out, errBuf bytes.Buffer
		code := runCLI([]string{"--dump"}, strings.NewReader(""), &out, &errBuf)
		if code != 0 {
			t.Fatalf("exit=%d want 0", code)
		}
		if !strings.Contains(out.String(), "Summary: 0 tables, 0 routes, 0 configs") {
			t.Errorf("unexpected empty dump output: %s", out.String())
		}
	})
}

func TestRunCLI_CheckModesAndFlags(t *testing.T) {
	root := writeProjectFiles(t, map[string]string{
		"config/app.go": `package config
import "github.com/goravel/framework/facades"
func init() {
	c := facades.Config()
	c.Add("app", map[string]any{"name": "Goravel"})
}
`,
		"app/models/user.go": `package models
type User struct {
	ID   uint   ` + "`gorm:\"column:id\"`" + `
	Name string ` + "`gorm:\"column:name\"`" + `
}
func (User) TableName() string { return "users" }
`,
		"resources/views/home.tmpl": `{{ define "home.tmpl" }}<h1>{{ .missing_var }}</h1>{{ end }}`,
		"app/repositories/user_repo.go": `package repositories
import "github.com/goravel/framework/facades"
func Bad() {
	_ = facades.DB().Table("users").Where("bad_col = ?", 1).Get(nil)
	_ = facades.Config().GetString("app.missing_cfg")
}
`,
		"skipme/broken.go": `package skipme
import "github.com/goravel/framework/facades"
func Bad() {
	_ = facades.DB().Table("users").Where("ignored_col = ?", 1).Get(nil)
}
`,
	})

	cleanRoot := writeProjectFiles(t, map[string]string{
		"config/app.go": `package config
import "github.com/goravel/framework/facades"
func init() {
	c := facades.Config()
	c.Add("app", map[string]any{"name": "Goravel"})
}
`,
		"app/models/user.go": `package models
type User struct {
	ID   uint   ` + "`gorm:\"column:id\"`" + `
	Name string ` + "`gorm:\"column:name\"`" + `
}
func (User) TableName() string { return "users" }
`,
		"app/repositories/user_repo.go": `package repositories
import "github.com/goravel/framework/facades"
func Good() {
	_ = facades.DB().Table("users").Where("name = ?", "alice").Get(nil)
	_ = facades.Config().GetString("app.name")
}
`,
		"resources/views/home.tmpl": `{{ define "home.tmpl" }}<h1>{{ .title }}</h1>{{ end }}`,
		"routes/web.go": `package routes
func Web(ctx http.Context) {
	_ = ctx.Response().View().Make("home.tmpl", map[string]any{"title": "Hi"})
}
`,
		"ignored_dir/bad.go": `package ignored_dir
import "github.com/goravel/framework/facades"
func Bad() {
	_ = facades.DB().Table("users").Where("nope = ?", 1).Get(nil)
}
`,
	})

	tests := []struct {
		name           string
		args           []string
		wantCode       int
		wantOutContain []string
		wantOutAbsent  []string
		wantErrContain string
	}{
		{
			name:           "clean project with skip-dirs",
			args:           []string{"check", "--skip-dirs=ignored_dir", cleanRoot},
			wantCode:       0,
			wantOutContain: []string{"0 issues"},
		},
		{
			name:           "check-sql finds SQL error only",
			args:           []string{"--check-sql", "--sql-source=model", "--out-format=text", "--skip-dirs=skipme", root},
			wantCode:       1,
			wantOutContain: []string{"bad_col"},
			wantOutAbsent:  []string{"app.missing_cfg", "missing_var", "ignored_col"},
		},
		{
			name:           "check-templates finds template error only",
			args:           []string{"--check-templates", "--out-format=text", "--skip-dirs=skipme", root},
			wantCode:       1,
			wantOutContain: []string{"missing_var"},
			wantOutAbsent:  []string{"bad_col", "app.missing_cfg"},
		},
		{
			name:           "run with --only-sql and --only-templates checks both",
			args:           []string{"run", "--sql", "--templates", "--out-format=github-actions", "--skip-dirs=skipme", root},
			wantCode:       1,
			wantOutContain: []string{"::error file=", "bad_col", "missing_var"},
			wantOutAbsent:  []string{"app.missing_cfg"},
		},
		{
			name:           "conflicting --only-sql then --all runs everything",
			args:           []string{"--check", "--only-sql", "--all", "--out-format=github-actions", "--skip-dirs=skipme", root},
			wantCode:       1,
			wantOutContain: []string{"::error ", "::warning ", "bad_col", "app.missing_cfg", "missing_var"},
		},
		{
			name:           "errors-only suppresses config warning",
			args:           []string{"check", "--all", "--errors-only", "--out-format=text", "--skip-dirs=skipme", root},
			wantCode:       1,
			wantOutContain: []string{"bad_col", "missing_var"},
			wantOutAbsent:  []string{"app.missing_cfg"},
		},
		{
			name:           "invalid sql-source returns exit 2 without terminating process",
			args:           []string{"check", "--sql-source=bogus", root},
			wantCode:       2,
			wantErrContain: `invalid --sql-source "bogus"`,
		},
		{
			name:           "missing sql-source value returns exit 2",
			args:           []string{"--check-sql", "--sql-source"},
			wantCode:       2,
			wantErrContain: "--sql-source requires a value",
		},
		{
			name:           "unknown flag ignored gracefully on clean project",
			args:           []string{"check", "--unknown-flag", "--skip-dirs=ignored_dir", cleanRoot},
			wantCode:       0,
			wantOutContain: []string{"0 issues"},
		},
		{
			name:           "live-db flag accepted in check mode",
			args:           []string{"--check-sql", "--live-db", "--skip-dirs=ignored_dir", cleanRoot},
			wantCode:       0,
			wantOutContain: []string{"0 issues"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			code := runCLI(tc.args, strings.NewReader(""), &out, &errBuf)
			if code != tc.wantCode {
				t.Errorf("exit=%d want %d\nstdout:\n%s\nstderr:\n%s", code, tc.wantCode, out.String(), errBuf.String())
			}
			for _, sub := range tc.wantOutContain {
				if !strings.Contains(out.String(), sub) {
					t.Errorf("stdout missing %q:\n%s", sub, out.String())
				}
			}
			for _, sub := range tc.wantOutAbsent {
				if strings.Contains(out.String(), sub) {
					t.Errorf("stdout unexpectedly contained %q:\n%s", sub, out.String())
				}
			}
			if tc.wantErrContain != "" && !strings.Contains(errBuf.String(), tc.wantErrContain) {
				t.Errorf("stderr missing %q:\n%s", tc.wantErrContain, errBuf.String())
			}
		})
	}
}

func TestRunCLI_LSPMode(t *testing.T) {
	t.Run("initialize shutdown exit with sql-source flag", func(t *testing.T) {
		session := makeLSPFrame(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}}) +
			makeLSPFrame(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"}) +
			makeLSPFrame(map[string]any{"jsonrpc": "2.0", "method": "exit"})
		var out, errBuf bytes.Buffer
		code := runCLI([]string{"--sql-source=model"}, strings.NewReader(session), &out, &errBuf)
		if code != 0 {
			t.Fatalf("exit=%d want 0 (stderr=%s)", code, errBuf.String())
		}
		if !strings.Contains(out.String(), `"goravel-lsp"`) {
			t.Errorf("expected initialize response, got: %s", out.String())
		}
	})

	t.Run("invalid sql-source flag in default LSP mode exits 2", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		code := runCLI([]string{"--sql-source", "nope"}, strings.NewReader(""), &out, &errBuf)
		if code != 2 {
			t.Fatalf("exit=%d want 2", code)
		}
		if !strings.Contains(errBuf.String(), "invalid --sql-source") {
			t.Errorf("expected error message, got: %s", errBuf.String())
		}
	})

	t.Run("malformed LSP header exits 1", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		code := runCLI(nil, strings.NewReader("Bad-Header: 1\r\n\r\n{}"), &out, &errBuf)
		if code != 1 {
			t.Fatalf("exit=%d want 1", code)
		}
		if !strings.Contains(errBuf.String(), "missing content-length") {
			t.Errorf("expected stderr error, got: %s", errBuf.String())
		}
	})
}

func TestCLI_SubprocessExecution(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "goravel-lsp")
	buildCmd := exec.Command("go", "build", "-ldflags=-X main.version=9.9.9-test", "-o", bin, ".")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	t.Run("version flag", func(t *testing.T) {
		out, err := exec.Command(bin, "--version").CombinedOutput()
		if err != nil {
			t.Fatalf("version failed: %v\n%s", err, out)
		}
		if strings.TrimSpace(string(out)) != "goravel-lsp version 9.9.9-test" {
			t.Errorf("unexpected version output: %q", string(out))
		}
	})

	t.Run("invalid sql-source exits with code 2", func(t *testing.T) {
		cmd := exec.Command(bin, "check", "--sql-source=invalid", ".")
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected non-zero exit, got 0\n%s", out)
		}
		exitErr, ok := err.(*exec.ExitError)
		if !ok || exitErr.ExitCode() != 2 {
			t.Errorf("expected exit code 2, got %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "invalid --sql-source") {
			t.Errorf("unexpected stderr: %s", out)
		}
	})

	t.Run("check-sql exits 1 on bad query", func(t *testing.T) {
		proj := writeProjectFiles(t, map[string]string{
			"app/models/item.go": `package models
type Item struct { ID uint ` + "`gorm:\"column:id\"`" + ` }
func (Item) TableName() string { return "items" }
`,
			"app/repo.go": `package app
import "github.com/goravel/framework/facades"
func f() { _ = facades.DB().Table("items").Where("ghost = ?", 1).Get(nil) }
`,
		})
		cmd := exec.Command(bin, "--check-sql", "--out-format=github-actions", proj)
		out, err := cmd.CombinedOutput()
		exitErr, ok := err.(*exec.ExitError)
		if !ok || exitErr.ExitCode() != 1 {
			t.Fatalf("expected exit code 1, got err=%v\n%s", err, out)
		}
		if !strings.Contains(string(out), "::error file=app/repo.go") || !strings.Contains(string(out), "ghost") {
			t.Errorf("unexpected check output:\n%s", out)
		}
	})

	t.Run("LSP stdio round trip", func(t *testing.T) {
		session := makeLSPFrame(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}}) +
			makeLSPFrame(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"}) +
			makeLSPFrame(map[string]any{"jsonrpc": "2.0", "method": "exit"})
		cmd := exec.Command(bin)
		cmd.Stdin = strings.NewReader(session)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("LSP subprocess failed: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), `"capabilities"`) {
			t.Errorf("expected LSP capabilities in output:\n%s", out)
		}
	})
}
