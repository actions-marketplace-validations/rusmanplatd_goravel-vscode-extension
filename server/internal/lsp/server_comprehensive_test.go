package lsp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

func buildFullLSPProject(t *testing.T) string {
	t.Helper()
	return project(t, map[string]string{
		".env": "APP_NAME=DemoApp\nJWT_SECRET=abc\n",
		"config/app.go": `package config
func init() {
	c := facades.Config()
	c.Add("app", map[string]any{"name": c.Env("APP_NAME", "Default")})
}
`,
		"lang/en.json": `{"hello": "Hello World"}`,
		"app/models/user.go": `package models
const UserOriginalTableName = "users"
type User struct {
	ID    uint    ` + "`gorm:\"column:id\"`" + `
	Email string  ` + "`gorm:\"column:email\"`" + `
	Bio   *string ` + "`gorm:\"column:bio\"`" + `
}
func (User) TableName() string { return UserOriginalTableName }
func ActiveUsers(q orm.Query) orm.Query { return q }

type UserRow struct {
	ID uint ` + "`db:\"id\"`" + `
}
`,
		"app/models/post.go": `package models
type Post struct {
	ID     uint  ` + "`gorm:\"column:id\"`" + `
	UserID uint  ` + "`gorm:\"column:user_id\"`" + `
	Title  string ` + "`gorm:\"column:title\"`" + `
	User   *User ` + "`gorm:\"foreignKey:UserID;references:ID\"`" + `
}
func (Post) TableName() string { return "posts" }
`,
		"database/migrations/20250101_create_users.go": `package migrations
type M20250101CreateUsers struct{}
func (m *M20250101CreateUsers) Up() error {
	return facades.Schema().Create("users", func(table schema.Blueprint) {
		table.ID()
		table.String("email")
		table.Text("bio").Nullable()
	})
}
`,
		"bootstrap/migrations.go": `package bootstrap
func Migrations() []schema.Migration {
	return []schema.Migration{
	}
}
`,
		"app/rules/slug.go": `package rules
type SlugRule struct{}
func (r *SlugRule) Signature() string { return "slug" }
func (r *SlugRule) Message() string { return "invalid slug" }
`,
		"app/http/middleware/auth.go": `package middleware
func JwtAuth() {}
`,
		"app/http/requests/create_user_request.go": `package requests
type CreateUserRequest struct {
	Email string ` + "`json:\"email\"`" + `
}
func (r *CreateUserRequest) Rules() map[string]string {
	return map[string]string{"email": "required|slug"}
}
`,
		"app/http/controllers/user_controller.go": `package controllers
type UserController struct{}
func (c *UserController) Store(ctx http.Context) {
	var req requests.CreateUserRequest
	_ = req
	_ = ctx.Response().View().Make("users/show.tmpl", map[string]any{"title": "Profile"})
}
`,
		"routes/api.go": `package routes
func Api() {
	ctrl := controllers.NewUserController()
	facades.Route().Middleware(middleware.JwtAuth()).Post("/users", ctrl.Store)
}
`,
		"app/console/commands/hello.go": `package commands
type HelloCmd struct{}
func (c *HelloCmd) Signature() string { return "app:hello" }
func (c *HelloCmd) Description() string { return "Say hello" }
`,
		"app/console/kernel.go": `package console
func Schedule() {
	facades.Schedule().Command("app:hello").Cron("0 */2 * * *")
}
`,
		"app/events/user_joined.go": `package events
type UserJoined struct{}
`,
		"app/listeners/notify.go": `package listeners
type NotifyAdmin struct{}
`,
		"app/providers/boot.go": `package providers
func Boot() {
	facades.Gate().Define("edit-user", func() {})
	facades.View().Share("site", "Goravel")
	_ = map[any][]any{
		&events.UserJoined{}: {&listeners.NotifyAdmin{}},
	}
}
`,
		"app/observers/user_observer.go": `package observers
type UserObserver struct{}
func (o *UserObserver) Created() error { return nil }
`,
		"resources/views/users/show.tmpl": `{{ define "users/show.tmpl" }}<h1>{{ .title }}</h1><p>{{ .site }}</p>{{ end }}`,
	})
}

func TestServer_CompletionHoverDefRefsRenameCodeActionsLensesHintsSymbolsTokens(t *testing.T) {
	root := buildFullLSPProject(t)

	repoSrc := `package repositories
func FindUsers(ctx http.Context) {
	uTbl := (models.User{}).TableName()
	_ = models.UserOriginalTableName
	var rows []models.UserRow
	q1 := ` + "`SELECT u.id, u.email AS mail FROM ` + uTbl + ` u JOIN posts p ON p.user_id = u.id ORDER BY mail`" + `
	_ = facades.DB().Select(&rows, q1)
	qStar := ` + "`SELECT * FROM users`" + `
	sqlIns := ` + "`INSERT INTO users (`" + `
	qJoin := ` + "`SELECT u.id FROM users u JOIN posts p ON `" + `
	sqlFrom := ` + "`SELECT id FROM `" + `
	_ = qStar; _ = sqlIns; _ = qJoin; _ = sqlFrom
	_ = facades.Config().GetString("app.name")
	_ = facades.Lang(ctx).Get("hello")
	_ = facades.Gate().Allows("edit-user")
	_ = facades.DB().Table("users").Where("email = ?", "a").Get(nil)
}
`
	repoURI := uri(filepath.Join(root, "app/repositories/u.go"))
	tmplPath := filepath.Join(root, "resources/views/users/show.tmpl")
	tmplBytes, _ := os.ReadFile(tmplPath)
	tmplSrc := string(tmplBytes)
	tmplURI := uri(tmplPath)
	ctrlPath := filepath.Join(root, "app/http/controllers/user_controller.go")
	ctrlBytes, _ := os.ReadFile(ctrlPath)
	ctrlURI := uri(ctrlPath)
	cmdPath := filepath.Join(root, "app/console/commands/hello.go")
	cmdURI := uri(cmdPath)
	evPath := filepath.Join(root, "app/events/user_joined.go")
	evURI := uri(evPath)
	modelPath := filepath.Join(root, "app/models/user.go")
	modelURI := uri(modelPath)
	kernelPath := filepath.Join(root, "app/console/kernel.go")
	kernelBytes, _ := os.ReadFile(kernelPath)
	kernelURI := uri(kernelPath)
	routePath := filepath.Join(root, "routes/api.go")
	routeBytes, _ := os.ReadFile(routePath)
	routeURI := uri(routePath)
	testGoURI := uri(filepath.Join(root, "tests/sample_test.go"))
	testGoSrc := "package tests\nimport \"testing\"\nfunc TestSample(t *testing.T) {}\n"
	unsupportedURI := uri(filepath.Join(root, "README.txt"))

	c := &rpc{t: t}
	c.call("initialize", map[string]any{
		"workspaceFolders":      []map[string]any{{"uri": uri(root)}},
		"initializationOptions": map[string]any{"sqlSource": "model"},
	})
	c.open(repoURI, repoSrc)
	c.open(tmplURI, tmplSrc)
	c.open(ctrlURI, string(ctrlBytes))
	c.open(kernelURI, string(kernelBytes))
	c.open(routeURI, string(routeBytes))
	c.open(testGoURI, testGoSrc)
	c.open(unsupportedURI, "hello plain text")

	// Completions across Raw SQL contexts, ORM, and template
	idCompQual := c.call("textDocument/completion", at(repoURI, posOf(repoSrc, "u.email", 3)))
	idCompJoin := c.call("textDocument/completion", at(repoURI, posOf(repoSrc, "JOIN posts p ON ", len("JOIN posts p ON "))))
	idCompFrom := c.call("textDocument/completion", at(repoURI, posOf(repoSrc, "SELECT id FROM ", len("SELECT id FROM "))))
	idCompIns := c.call("textDocument/completion", at(repoURI, posOf(repoSrc, "INSERT INTO users (", len("INSERT INTO users ("))))
	idCompOrder := c.call("textDocument/completion", at(repoURI, posOf(repoSrc, "ORDER BY mail", len("ORDER BY "))))
	idCompTmpl := c.call("textDocument/completion", at(tmplURI, posOf(tmplSrc, ".title", 1)))
	idCompUnsupported := c.call("textDocument/completion", at(unsupportedURI, map[string]int{"line": 0, "character": 2}))

	// Hover across Raw SQL, TableConst, VarRef, Template, Schedule
	idHoverConst := c.call("textDocument/hover", at(repoURI, posOf(repoSrc, "UserOriginalTableName", 2)))
	idHoverVarRef := c.call("textDocument/hover", at(repoURI, posOf(repoSrc, "+ uTbl +", 3)))
	idHoverColAlias := c.call("textDocument/hover", at(repoURI, posOf(repoSrc, "AS mail", 4)))
	idHoverTblAlias := c.call("textDocument/hover", at(repoURI, posOf(repoSrc, "u JOIN", 0)))
	idHoverTmpl := c.call("textDocument/hover", at(tmplURI, posOf(tmplSrc, ".title", 2)))
	idHoverCron := c.call("textDocument/hover", at(kernelURI, posOf(string(kernelBytes), "0 */2", 2)))

	// Definitions: TableConst, VarRef, SQL col/alias, Middleware, Template
	idDefConst := c.call("textDocument/definition", at(repoURI, posOf(repoSrc, "UserOriginalTableName", 2)))
	idDefVarRef := c.call("textDocument/definition", at(repoURI, posOf(repoSrc, "+ uTbl +", 3)))
	idDefSQLCol := c.call("textDocument/definition", at(repoURI, posOf(repoSrc, "u.email", 3)))
	idDefAliasUse := c.call("textDocument/definition", at(repoURI, posOf(repoSrc, "ORDER BY mail", 10)))
	idDefMw := c.call("textDocument/definition", at(routeURI, posOf(string(routeBytes), "JwtAuth", 2)))
	idDefTmpl := c.call("textDocument/definition", at(tmplURI, posOf(tmplSrc, ".title", 2)))

	// CodeActions: ExpandStar, GenerateRowStruct + AddMissingScanFields, Template
	starPos := posOf(repoSrc, "SELECT * FROM users", 7)
	idActStar := c.call("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": repoURI},
		"range":        map[string]any{"start": starPos, "end": starPos},
	})
	selPos := posOf(repoSrc, "facades.DB().Select(&rows, q1)", 15)
	idActQuery := c.call("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": repoURI},
		"range":        map[string]any{"start": selPos, "end": selPos},
	})

	// CodeLens across controller, command, event, model (observer+scope), test file, template
	idLensCtrl := c.call("textDocument/codeLens", map[string]any{"textDocument": map[string]any{"uri": ctrlURI}})
	idLensCmd := c.call("textDocument/codeLens", map[string]any{"textDocument": map[string]any{"uri": cmdURI}})
	idLensEv := c.call("textDocument/codeLens", map[string]any{"textDocument": map[string]any{"uri": evURI}})
	idLensModel := c.call("textDocument/codeLens", map[string]any{"textDocument": map[string]any{"uri": modelURI}})
	idLensTest := c.call("textDocument/codeLens", map[string]any{"textDocument": map[string]any{"uri": testGoURI}})
	idLensTmpl := c.call("textDocument/codeLens", map[string]any{"textDocument": map[string]any{"uri": tmplURI}})

	// InlayHints: repo (config+lang), kernel (cron), routes, template
	idHintsRepo := c.call("textDocument/inlayHint", map[string]any{"textDocument": map[string]any{"uri": repoURI}})
	idHintsKernel := c.call("textDocument/inlayHint", map[string]any{"textDocument": map[string]any{"uri": kernelURI}})
	idHintsRoutes := c.call("textDocument/inlayHint", map[string]any{"textDocument": map[string]any{"uri": routeURI}})
	idHintsTmpl := c.call("textDocument/inlayHint", map[string]any{"textDocument": map[string]any{"uri": tmplURI}})

	// DocumentSymbol (template vs Go)
	idSymTmpl := c.call("textDocument/documentSymbol", map[string]any{"textDocument": map[string]any{"uri": tmplURI}})
	idSymGo := c.call("textDocument/documentSymbol", map[string]any{"textDocument": map[string]any{"uri": repoURI}})

	// SemanticTokens
	idSemGo := c.call("textDocument/semanticTokens/full", map[string]any{"textDocument": map[string]any{"uri": repoURI}})
	idSemTxt := c.call("textDocument/semanticTokens/full", map[string]any{"textDocument": map[string]any{"uri": unsupportedURI}})

	// Custom requests: overview, erDiagram, routeSample, registerBootstrap, explainQuery, previewQuery
	idOverview := c.call("goravel/overview", map[string]any{})
	idER := c.call("goravel/erDiagram", map[string]any{})
	idSample := c.call("goravel/routeSample", map[string]any{"controller": "UserController", "action": "Store"})
	idRegBootOK := c.call("goravel/registerBootstrap", map[string]any{"file": filepath.Join(root, "database/migrations/20250101_create_users.go")})
	idRegBootAlready := c.call("goravel/registerBootstrap", map[string]any{"file": filepath.Join(root, "database/migrations/20250101_create_users.go")})
	idExplainErr := c.call("goravel/explainQuery", map[string]any{"sql": "SELECT 1"})
	idPreviewErr := c.call("goravel/previewQuery", map[string]any{"sql": "DELETE FROM users"})

	// References & Rename (table alias, column, template var, gate, lang)
	idRefCol := c.call("textDocument/references", at(repoURI, posOf(repoSrc, `Where("email = ?"`, 8)))
	idRefTmplVar := c.call("textDocument/references", at(tmplURI, posOf(tmplSrc, ".title", 2)))
	pRenAlias := at(repoURI, posOf(repoSrc, "u JOIN", 0))
	pRenAlias["newName"] = "usr"
	idRenAlias := c.call("textDocument/rename", pRenAlias)
	pRenGate := at(repoURI, posOf(repoSrc, `"edit-user"`, 2))
	pRenGate["newName"] = "manage-user"
	idRenGate := c.call("textDocument/rename", pRenGate)
	pRenTmplVar := at(tmplURI, posOf(tmplSrc, ".title", 2))
	pRenTmplVar["newName"] = "heading"
	idRenTmplVar := c.call("textDocument/rename", pRenTmplVar)

	// Workspace notifications: didSave (indexed vs non-indexed), didChangeWatchedFiles, didChangeConfiguration
	c.notify("textDocument/didSave", map[string]any{"textDocument": map[string]any{"uri": modelURI}})
	c.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []any{}})
	c.notify("workspace/didChangeConfiguration", map[string]any{
		"settings": map[string]any{"goravel": map[string]any{"sql": map[string]any{"liveIntrospection": false, "source": "both"}}},
	})

	rs := c.run()

	assertContains := func(name string, id int, subs ...string) {
		t.Helper()
		r := find(rs, id)
		if r == nil || r.Error != nil {
			t.Fatalf("%s (id=%d) failed: %+v", name, id, r)
		}
		resStr := string(r.Result)
		for _, sub := range subs {
			if !strings.Contains(resStr, sub) {
				t.Errorf("%s missing %q in:\n%s", name, sub, resStr)
			}
		}
	}

	assertContains("compQual", idCompQual, `"email"`, `"bio"`)
	assertContains("compJoin", idCompJoin, `"p.user_id = u.id"`)
	assertContains("compFrom", idCompFrom, `"uTbl"`, `"users"`)
	assertContains("compIns", idCompIns, `"email"`)
	assertContains("compOrder", idCompOrder, `"mail"`)
	assertContains("compTmpl", idCompTmpl, `"title"`, `"site"`)
	if r := find(rs, idCompUnsupported); r == nil || string(r.Result) != "null" {
		t.Errorf("unsupported file completion should be null, got %+v", r)
	}

	assertContains("hoverConst", idHoverConst, "UserOriginalTableName", "users")
	assertContains("hoverVarRef", idHoverVarRef, "users", "models.User")
	assertContains("hoverColAlias", idHoverColAlias, "Column Alias `mail`")
	assertContains("hoverTblAlias", idHoverTblAlias, "Table Alias `u`")
	assertContains("hoverTmpl", idHoverTmpl, ".title")
	assertContains("hoverCron", idHoverCron, "Every 2 hours")

	assertContains("defConst", idDefConst, "user.go")
	assertContains("defVarRef", idDefVarRef, "user.go")
	assertContains("defSQLCol", idDefSQLCol, "user.go")
	assertContains("defAliasUse", idDefAliasUse, "u.go")
	assertContains("defMw", idDefMw, "auth.go")
	assertContains("defTmpl", idDefTmpl, "user_controller.go")

	assertContains("actStar", idActStar, "Expand * to explicit columns", "id, email, bio")
	assertContains("actQuery", idActQuery, "Generate row struct FindUsersRow", "Add 1 missing scan field(s) to UserRow")

	assertContains("lensCtrl", idLensCtrl, "POST /users", "Test Request")
	assertContains("lensCmd", idLensCmd, "Run artisan app:hello")
	assertContains("lensEv", idLensEv, "1 Listener(s): NotifyAdmin")
	assertContains("lensModel", idLensModel, "Observer: UserObserver", "1 Scope(s): ActiveUsers")
	assertContains("lensTest", idLensTest, "Run Goravel Test", "TestSample")
	assertContains("lensTmpl", idLensTmpl, "POST /users")

	assertContains("hintsRepo", idHintsRepo, "APP_NAME", "Hello World")
	assertContains("hintsKernel", idHintsKernel, "Every 2 hours")
	assertContains("hintsRoutes", idHintsRoutes, "POST /users")
	_ = find(rs, idHintsTmpl)

	assertContains("symTmpl", idSymTmpl, `define \"users/show.tmpl\"`)
	if r := find(rs, idSymGo); r == nil || string(r.Result) != "null" {
		t.Errorf("Go documentSymbol should be null, got %+v", r)
	}
	assertContains("semGo", idSemGo, `"data":[`)
	if r := find(rs, idSemTxt); r == nil || string(r.Result) != `{"data":[]}` {
		t.Errorf("non-Go semantic tokens should be empty data, got %+v", r)
	}

	assertContains("overview", idOverview, `"users"`, `"app:hello"`, `"UserObserver"`, `"users/show.tmpl"`)
	assertContains("erDiagram", idER, "erDiagram", "posts }o--|| users")
	assertContains("routeSample", idSample, `email`)
	assertContains("regBootOK", idRegBootOK, `"ok":true`, "Register M20250101CreateUsers")
	assertContains("regBootAlready", idRegBootAlready, `"ok":false`)
	assertContains("explainErr", idExplainErr, `"ok":false`)
	assertContains("previewErr", idPreviewErr, `"ok":false`, "SELECT or WITH")

	// Verify bootstrap/migrations.go was actually updated on disk
	migBootBytes, _ := os.ReadFile(filepath.Join(root, "bootstrap/migrations.go"))
	if !strings.Contains(string(migBootBytes), "&migrations.M20250101CreateUsers{}") {
		t.Errorf("expected bootstrap/migrations.go to contain registered migration, got:\n%s", migBootBytes)
	}

	assertContains("refCol", idRefCol, "u.go")
	assertContains("refTmplVar", idRefTmplVar, "show.tmpl", "user_controller.go")
	assertContains("renAlias", idRenAlias, `"newText":"usr"`)
	assertContains("renGate", idRenGate, `"newText":"manage-user"`, "boot.go")
	assertContains("renTmplVar", idRenTmplVar, `"newText":"heading"`, "show.tmpl", "user_controller.go")
}

func TestServer_UTF16PositionsAndSettingsHelpers(t *testing.T) {
	// Emoji 😀 is 4 UTF-8 bytes and 2 UTF-16 code units
	text := "a😀b\ncd"
	offB := strings.Index(text, "b")
	posB := offsetToPos(text, offB)
	if posB.Line != 0 || posB.Character != 3 {
		t.Fatalf("offsetToPos('b') = %+v want {Line:0 Character:3}", posB)
	}
	if back := posToOffset(text, posB); back != offB {
		t.Errorf("posToOffset roundtrip = %d want %d", back, offB)
	}
	if p := offsetToPos(text, -5); p.Line != 0 || p.Character != 0 {
		t.Errorf("negative offset clamp failed: %+v", p)
	}
	if p := offsetToPos(text, 999); p.Line != 1 || p.Character != 2 {
		t.Errorf("oversized offset clamp failed: %+v", p)
	}

	// Settings extractors (nested vs flat vs nil)
	if v, ok := extractLiveIntrospectionSetting(map[string]any{"goravel": map[string]any{"sql": map[string]any{"liveIntrospection": true}}}); !ok || !v {
		t.Error("expected nested liveIntrospection=true")
	}
	if v, ok := extractLiveIntrospectionSetting(map[string]any{"liveIntrospection": true}); !ok || !v {
		t.Error("expected flat liveIntrospection=true")
	}
	if _, ok := extractLiveIntrospectionSetting(nil); ok {
		t.Error("nil settings must return ok=false")
	}
	if src, ok := extractSQLSourceSetting(map[string]any{"goravel": map[string]any{"sql": map[string]any{"source": "live-db"}}}); !ok || src != indexer.SQLSourceLiveDB {
		t.Errorf("expected nested sqlSource=live-db, got %q ok=%v", src, ok)
	}
	if src, ok := extractSQLSourceSetting(map[string]any{"sqlSource": "model"}); !ok || src != indexer.SQLSourceModel {
		t.Errorf("expected flat sqlSource=model, got %q ok=%v", src, ok)
	}

	// Check wrappers & GitHub Actions output format
	var buf bytes.Buffer
	proj := project(t, map[string]string{
		"app/models/u.go": "package models\ntype U struct{ ID uint }\nfunc (U) TableName() string { return \"u\" }\n",
		"app/r.go":        repo(`_ = facades.DB().Table("u").Where("bad = ?", 1).Get(nil)`),
	})
	code := CheckWithOptions(CheckOptions{Root: proj, OnlySQL: true, OutFormat: "github-actions", Stdout: &buf})
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(buf.String(), "::error file=app/r.go,line=") {
		t.Errorf("expected GHA annotation output, got:\n%s", buf.String())
	}
	// Verify helper wrappers Check, CheckSQL, CheckTemplates execute cleanly on empty dir
	empty := project(t, nil)
	if Check(empty) != 0 || CheckSQL(empty) != 0 || CheckTemplates(empty) != 0 {
		t.Error("empty project wrappers must all return 0")
	}
	// Multi-root isolation check
	srv := New(strings.NewReader(""), &bytes.Buffer{})
	srv.SetSQLSource("")
	if srv.sqlSource != indexer.SQLSourceAuto {
		t.Errorf("empty SetSQLSource should default to auto, got %q", srv.sqlSource)
	}
	srv.addRoot(proj)
	srv.addRoot(proj) // duplicate ignored
	srv.addRoot(".")  // ignored
	if len(srv.roots) != 1 {
		t.Errorf("expected 1 unique root, got %v", srv.roots)
	}
	if sanitizeMermaidIdent("a-b.c!") != "a_b_c" || sanitizeMermaidIdent("!!!") != "unknown" {
		t.Errorf("unexpected sanitizeMermaidIdent results")
	}
	// Verify JSON validity of overview on empty server
	b, err := json.Marshal(srv.overview())
	if err != nil || !strings.Contains(string(b), `"tables"`) {
		t.Errorf("overview marshal failed: %v %s", err, b)
	}
}
