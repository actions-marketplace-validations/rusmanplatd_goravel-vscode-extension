package framework

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

func makeFrameworkProject(t *testing.T, files map[string]string) (string, *indexer.Catalog) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, indexer.Build(root)
}

func TestDrift_CodeActionsBootstrapAndModelMigrationSync(t *testing.T) {
	modelSrc := `package models
type Article struct {
	ID      uint   ` + "`gorm:\"column:id\"`" + `
	Title   string ` + "`gorm:\"column:title\"`" + `
	Summary *string ` + "`gorm:\"column:summary\"`" + `
	Views   int64  ` + "`gorm:\"column:views\"`" + `
	Active  bool   ` + "`gorm:\"column:active\"`" + `
	Score   float64 ` + "`gorm:\"column:score\"`" + `
}
func (Article) TableName() string { return "articles" }
`
	migBlueprintSrc := `package migrations
type M20250101CreateArticles struct{}
func (r *M20250101CreateArticles) Up() error {
	return facades.Schema().Create("articles", func(table schema.Blueprint) {
		table.ID()
		table.String("title")
		table.BigInteger("author_id")
		table.Text("body").Nullable()
	})
}
func (r *M20250101CreateArticles) Down() error { return nil }
`
	rawMigSrc := `package migrations_extra
type M20250102CreateLogs struct{}
func (r *M20250102CreateLogs) Up() error {
	return facades.DB().Statement(` + "`CREATE TABLE logs (id UUID PRIMARY KEY, msg TEXT NOT NULL)`" + `)
}
`
	logModelSrc := `package models
type Log struct {
	ID    uuid.UUID ` + "`gorm:\"column:id\"`" + `
	Msg   string    ` + "`gorm:\"column:msg\"`" + `
	Level int       ` + "`gorm:\"column:level\"`" + `
	Meta  *time.Time ` + "`gorm:\"column:meta\"`" + `
}
func (Log) TableName() string { return "logs" }
`
	root, cat := makeFrameworkProject(t, map[string]string{
		"app/models/article.go":                      modelSrc,
		"app/models/log.go":                          logModelSrc,
		"database/migrations/20250101_articles.go":   migBlueprintSrc,
		"database/migrations_extra/20250102_logs.go": rawMigSrc,
		"database/seeders/article_seeder.go":         "package seeders\ntype ArticleSeeder struct{}\nfunc (s *ArticleSeeder) Signature() string { return \"ArticleSeeder\" }\n",
		"app/rules/custom_rule.go":                   "package rules\ntype CustomRule struct{}\nfunc (r *CustomRule) Signature() string { return \"custom_rule\" }\n",
		"app/console/commands/sync.go":               "package commands\ntype SyncCmd struct{}\nfunc (c *SyncCmd) Signature() string { return \"app:sync\" }\n",
		"bootstrap/migrations.go":                    "package bootstrap\nfunc Migrations() []schema.Migration {\n\treturn []schema.Migration{\n\t}\n}\n",
		"bootstrap/bootstrap_extra/migrations.go":    "package bootstrap_extra\nfunc Migrations() []schema.Migration {\n\treturn []schema.Migration{\n\t}\n}\n",
		"bootstrap/seeders.go":                       "package bootstrap\nfunc Seeders() []seeder.Seeder {\n\treturn []seeder.Seeder{\n\t}\n}\n",
		"bootstrap/rules.go":                         "package bootstrap\nfunc Rules() []validation.Rule {\n\treturn []validation.Rule{\n\t}\n}\n",
		"bootstrap/commands.go":                      "package bootstrap\nfunc Commands() []console.Command {\n\treturn []console.Command{\n\t}\n}\n",
	})

	// 1. Unregistered bootstrap diagnostics & code actions across kinds
	migPath := filepath.Join(root, "database/migrations/20250101_articles.go")
	if ds := DriftDiagnostics(migPath, migBlueprintSrc, cat); len(ds) == 0 || !strings.Contains(ds[0].Message, "bootstrap/") {
		t.Errorf("expected unregistered bootstrap diagnostic, got %+v", ds)
	}
	for _, rel := range []string{
		"database/migrations/20250101_articles.go",
		"database/migrations_extra/20250102_logs.go",
		"database/seeders/article_seeder.go",
		"app/rules/custom_rule.go",
		"app/console/commands/sync.go",
	} {
		fp := filepath.Join(root, filepath.FromSlash(rel))
		b, _ := os.ReadFile(fp)
		acts := DriftCodeActions(fp, string(b), cat, 0)
		if len(acts) == 0 || !strings.Contains(acts[0].Title, "Register ") {
			t.Errorf("expected bootstrap registration action for %s, got %+v", rel, acts)
		}
	}

	// 2. Model <-> Blueprint migration bidirectional quick fixes
	artPath := filepath.Join(root, "app/models/article.go")
	acts := DriftCodeActions(artPath, modelSrc, cat, strings.Index(modelSrc, "Article"))
	if len(acts) != 2 {
		t.Fatalf("expected 2 drift actions (mig->model & model->mig), got %+v", acts)
	}
	if !strings.Contains(acts[0].Edits[0].NewText, "AuthorID int64") || !strings.Contains(acts[0].Edits[0].NewText, "Body *string") {
		t.Errorf("unexpected mig->model edit: %q", acts[0].Edits[0].NewText)
	}
	if !strings.Contains(acts[1].Edits[0].NewText, `table.String("summary").Nullable()`) || !strings.Contains(acts[1].Edits[0].NewText, `table.BigInteger("views")`) {
		t.Errorf("unexpected model->blueprint edit: %q", acts[1].Edits[0].NewText)
	}

	// 3. Model -> Raw SQL CREATE TABLE quick fix
	logPath := filepath.Join(root, "app/models/log.go")
	logActs := DriftCodeActions(logPath, logModelSrc, cat, strings.Index(logModelSrc, "Log"))
	if len(logActs) != 1 || !strings.Contains(logActs[0].Edits[0].NewText, "level INTEGER NOT NULL") || !strings.Contains(logActs[0].Edits[0].NewText, "meta TIMESTAMPTZ") {
		t.Errorf("unexpected model->rawSQL edit: %+v", logActs)
	}
}

func TestRequests_CodeActionsMessagesAndAttributes(t *testing.T) {
	reqSrc := `package requests
type CreateOrderRequest struct {
	Name   string    ` + "`json:\"name\"`" + `
	Amount int64     ` + "`json:\"amount\"`" + `
	Rate   *float64  ` + "`json:\"rate\"`" + `
	Active bool      ` + "`json:\"active\"`" + `
	RefID  uuid.UUID ` + "`json:\"ref_id\"`" + `
	Tags   []string  ` + "`json:\"tags\"`" + `
}
func (r *CreateOrderRequest) Rules() map[string]string {
	return map[string]string{
		"name": "required|string",
	}
}
func (r *CreateOrderRequest) Messages() map[string]string {
	return map[string]string{
		"name.required": "Name required",
		"ghost.required": "Bad key",
	}
}
`
	root, cat := makeFrameworkProject(t, map[string]string{
		"app/http/requests/order_request.go": reqSrc,
	})
	fp := filepath.Join(root, "app/http/requests/order_request.go")
	ds := RequestDiagnostics(fp, reqSrc, cat)
	if len(ds) != 1 || !strings.Contains(ds[0].Message, "'ghost'") {
		t.Errorf("expected 1 diagnostic for ghost in Messages(), got %+v", ds)
	}

	acts := RequestCodeActions(fp, reqSrc, cat, strings.Index(reqSrc, `"name":`))
	if len(acts) != 1 {
		t.Fatalf("expected 1 RequestCodeAction, got %+v", acts)
	}
	edit := acts[0].Edits[0].NewText
	for _, want := range []string{
		`"amount": "required|integer"`,
		`"rate": "nullable|numeric"`,
		`"active": "required|boolean"`,
		`"ref_id": "required|uuid"`,
		`"tags": "nullable"`,
	} {
		if !strings.Contains(edit, want) {
			t.Errorf("generated Rules missing %s:\n%s", want, edit)
		}
	}
}

func TestControllerAndRoutes_CodeActionsHoverInlayHintsDefAndSample(t *testing.T) {
	ctrlSrc := `package controllers
type ItemController struct{}

func (c *ItemController) Store(ctx http.Context) {
	var req requests.CreateItemRequest
	_ = req
	_ = ctx.Request().QueryInt("page")
	_ = ctx.Request().QueryBool("dry_run")
	var item models.Item
	_ = item
}

func (c *ItemController) Index(ctx http.Context) {
	var req requests.CreateItemRequest
	_ = req
	var items []models.Item
	_ = items
}

// @Router /wrong/path [delete]
func (c *ItemController) Show(ctx http.Context) {
	_ = ctx.Request().Route("id")
	_ = facades.Config().Env("NEW_API_KEY")
	_ = facades.Lang(ctx).Get("missing.greeting")
}
`
	routeSrc := `package routes
func Api() {
	ctrl := controllers.NewItemController()
	facades.Route().Middleware(middleware.JwtAuth()).Post("/items", ctrl.Store)
	facades.Route().Get("/items", ctrl.Index)
	facades.Route().Get("/items/:id", ctrl.Show)
}
`
	root, cat := makeFrameworkProject(t, map[string]string{
		".env.example": "APP_NAME=Test\n",
		"lang/en.json": "{\n  \"hello\": \"Hi\"\n}\n",
		"app/http/middleware/jwt.go": `package middleware
func JwtAuth() {}
`,
		"app/http/requests/item_request.go": `package requests
type CreateItemRequest struct {
	Title string    ` + "`json:\"title\"`" + `
	Count int       ` + "`json:\"count\"`" + `
	Price float64   ` + "`json:\"price\"`" + `
	Live  bool      ` + "`json:\"live\"`" + `
	UID   uuid.UUID ` + "`json:\"uid\"`" + `
	Tags  []string  ` + "`json:\"tags\"`" + `
}
`,
		"app/http/controllers/item_controller.go": ctrlSrc,
		"routes/api.go":                           routeSrc,
	})

	ctrlPath := filepath.Join(root, "app/http/controllers/item_controller.go")
	routePath := filepath.Join(root, "routes/api.go")

	// Generate Swagger for POST Store (201, BearerAuth, query params, request body, model object)
	storeActs := ControllerCodeActions(ctrlPath, ctrlSrc, cat, strings.Index(ctrlSrc, "func (c *ItemController) Store"))
	if len(storeActs) != 1 {
		t.Fatalf("expected Swagger generate action for Store, got %+v", storeActs)
	}
	swagStore := storeActs[0].Edits[0].NewText
	for _, want := range []string{"@Security BearerAuth", "@Param page query integer false", "@Param dry_run query boolean false", "@Param request body requests.CreateItemRequest", "@Success 201 {object} models.Item", "@Router /items [post]"} {
		if !strings.Contains(swagStore, want) {
			t.Errorf("Store swagger missing %q:\n%s", want, swagStore)
		}
	}

	// Generate Swagger for GET Index (expands request struct fields as query params + array model)
	idxActs := ControllerCodeActions(ctrlPath, ctrlSrc, cat, strings.Index(ctrlSrc, "func (c *ItemController) Index"))
	if len(idxActs) != 1 || !strings.Contains(idxActs[0].Edits[0].NewText, "@Param count query integer true") || !strings.Contains(idxActs[0].Edits[0].NewText, "@Success 200 {array} models.Item") {
		t.Errorf("unexpected GET Index swagger action: %+v", idxActs)
	}

	// Update mismatched @Router on Show
	showActs := ControllerCodeActions(ctrlPath, ctrlSrc, cat, strings.Index(ctrlSrc, "@Router /wrong/path"))
	if len(showActs) != 1 || showActs[0].Edits[0].NewText != "@Router /items/:id [get]" {
		t.Errorf("expected @Router update quickfix, got %+v", showActs)
	}

	// RouteAndControllerHover on controller method, middleware, and route line
	if h := RouteAndControllerHover(ctrlPath, ctrlSrc, cat, strings.Index(ctrlSrc, "Store(")+1); h == nil || !strings.Contains(h.Markdown, "POST /items") || !strings.Contains(h.Markdown, "JwtAuth") {
		t.Errorf("controller hover failed: %+v", h)
	}
	if h := RouteAndControllerHover(routePath, routeSrc, cat, strings.Index(routeSrc, "JwtAuth")+2); h == nil || !strings.Contains(h.Markdown, "Middleware `JwtAuth`") {
		t.Errorf("middleware hover failed: %+v", h)
	}
	if h := RouteAndControllerHover(routePath, routeSrc, cat, strings.Index(routeSrc, `Get("/items"`)+2); h == nil || !strings.Contains(h.Markdown, "Route `GET /items`") {
		t.Errorf("route line hover failed: %+v", h)
	}

	// RouteInlayHints & RouteMiddlewareDefinition
	hints := RouteInlayHints(routePath, routeSrc, cat)
	if len(hints) != 3 {
		t.Errorf("expected 3 route inlay hints, got %+v", hints)
	}
	if def := RouteMiddlewareDefinition(routePath, routeSrc, cat, strings.Index(routeSrc, "JwtAuth")+2); def == nil || !strings.HasSuffix(def.Loc.File, "jwt.go") {
		t.Errorf("middleware definition failed: %+v", def)
	}

	// EnvAndLangCodeActions
	envActs := EnvAndLangCodeActions(ctrlPath, ctrlSrc, cat, strings.Index(ctrlSrc, `Env("NEW_API_KEY")`)+2)
	if len(envActs) != 1 || !strings.Contains(envActs[0].Edits[0].NewText, "NEW_API_KEY=") {
		t.Errorf("expected .env.example quickfix, got %+v", envActs)
	}
	langActs := EnvAndLangCodeActions(ctrlPath, ctrlSrc, cat, strings.Index(ctrlSrc, `Lang(ctx).Get("missing.greeting")`)+2)
	if len(langActs) != 1 || !strings.Contains(langActs[0].Edits[0].NewText, `"missing.greeting": "missing.greeting"`) {
		t.Errorf("expected lang/en.json quickfix, got %+v", langActs)
	}

	// SampleBodyForRoute
	sample := SampleBodyForRoute(cat, "ItemController", "Store")
	for _, want := range []string{`"title": ""`, `"count": 0`, `"price": 0`, `"live": false`, `"uid": "00000000-0000-0000-0000-000000000000"`, `"tags": []`} {
		if !strings.Contains(sample, want) {
			t.Errorf("SampleBodyForRoute missing %s:\n%s", want, sample)
		}
	}
}

func TestViewsAndTemplates_FullIntelligenceSuite(t *testing.T) {
	goCtrl := `package controllers
type PageController struct{}
func (c *PageController) Show(ctx http.Context) {
	var user models.User
	var users []models.User
	_ = facades.View().Shared("app_name")
	_ = facades.View().Shared("unknown_shared")
	_ = facades.View().GetShared()
	_ = facades.View().RegisteredViews()
	_ = ctx.Response().View().Make("pages/show.tmpl", map[string]any{
		"title": "Dashboard",
		"user":  user,
		"users": users,
	})
	_ = ctx.Response().View().Make("pages/missing_new.tmpl", map[string]any{"x": 1})
}
`
	showTmpl := `{{ define "pages/show.tmpl" }}
{{ template "partials/nav.tmpl" . }}
<a href="/pages">Home</a>
<link href="/css/app.css" rel="stylesheet">
<link href="/css/missing.css" rel="stylesheet">
<h1>{{ .title | upper }}</h1>
<p>{{ .user.Email }}</p>
<p>{{ .csrf_token }}</p>
{{ $u := .user }}
<span>{{ $u.Email }}</span>
{{ range $idx, $item := .users }}
  <li>{{ $item.Email }} - {{ .Email }} - {{ $.title }}</li>
{{ end }}
{{ with .user }}
  <em>{{ .Email }}</em>
{{ end }}
<p>{{ .titl }}</p>
<p>{{ badFunc .title }}</p>
<p>{{ join .title }}</p>
{{ end }}
`
	navTmpl := `{{ define "partials/nav.tmpl" }}<nav>{{ .app_name }} {{ .nav_extra }}</nav>{{ end }}`

	root, cat := makeFrameworkProject(t, map[string]string{
		"config/app.go": `package config
func init() {
	facades.Config().Add("app", map[string]any{"name": "Goravel"})
}
`,
		"app/models/user.go": `package models
type User struct {
	ID    uint   ` + "`gorm:\"column:id\"`" + `
	Email string ` + "`gorm:\"column:email\"`" + `
}
func (User) TableName() string { return "users" }
`,
		"app/providers/view_provider.go": `package providers
func Boot() {
	facades.View().Share("app_name", "Goravel")
	_ = template.FuncMap{
		"upper": func(s string) string { return s },
		"join":  func(a string, b string) string { return a + b },
	}
}
`,
		"routes/web.go": `package routes
func Web() {
	ctrl := controllers.NewPageController()
	facades.Route().Get("/pages", ctrl.Show)
}
`,
		"public/css/app.css":                      "body{}",
		"packages/pkg/views/pages/show.tmpl":      "{{ define \"pages/show.tmpl\" }}PKG{{ end }}",
		"resources/views/pages/show.tmpl":         showTmpl,
		"resources/views/partials/nav.tmpl":       navTmpl,
		"resources/views/orphan/empty.tmpl":       "   ",
		"resources/views/orphan/wrong_def.tmpl":   "{{ define \"wrong_name\" }}hi{{ end }}",
		"app/http/controllers/page_controller.go": goCtrl,
	})

	ctrlPath := filepath.Join(root, "app/http/controllers/page_controller.go")
	showPath := filepath.Join(root, "resources/views/pages/show.tmpl")
	navPath := filepath.Join(root, "resources/views/partials/nav.tmpl")
	emptyPath := filepath.Join(root, "resources/views/orphan/empty.tmpl")
	wrongDefPath := filepath.Join(root, "resources/views/orphan/wrong_def.tmpl")

	// 1. Go ViewCompletions (including regression check for Bug #4 markdown formatting)
	compViews := ViewCompletions(ctrlPath, goCtrl, cat, strings.Index(goCtrl, `"pages/show.tmpl"`)+2)
	foundShowComp := false
	for _, it := range compViews {
		if it.Label == "pages/show.tmpl" {
			foundShowComp = true
			if strings.Contains(it.Doc, "`.`") {
				t.Errorf("regression: ViewCompletions doc has malformed backticks: %q", it.Doc)
			}
		}
	}
	if !foundShowComp {
		t.Errorf("expected pages/show.tmpl in ViewCompletions, got %+v", compViews)
	}
	if sItems := ViewCompletions(ctrlPath, goCtrl, cat, strings.Index(goCtrl, `"app_name"`)+2); len(sItems) == 0 || sItems[0].Label != "app_name" {
		t.Errorf("expected shared key completion, got %+v", sItems)
	}
	if dItems := ViewCompletions(ctrlPath, goCtrl, cat, strings.Index(goCtrl, `"title":`)); len(dItems) == 0 {
		t.Errorf("expected missing data map key completion (nav_extra), got %+v", dItems)
	}

	// 2. Go ViewHover, ViewLocations/ViewDefinition, ViewDiagnostics, ViewCodeActions, ViewInlayHints
	for _, tc := range []struct {
		needle string
		want   string
	}{
		{`"pages/show.tmpl"`, "Overrides package view"},
		{`"pages/missing_new.tmpl"`, "not found"},
		{`"app_name"`, "Shared View Variable `.app_name`"},
		{`GetShared()`, "Registered shared variables"},
		{`RegisteredViews()`, "templates)"},
		{`"title":`, "View Context Key `.title`"},
	} {
		h := ViewHover(ctrlPath, goCtrl, cat, strings.Index(goCtrl, tc.needle)+2)
		if h == nil || !strings.Contains(h.Markdown, tc.want) {
			t.Errorf("ViewHover(%q) = %+v want %q", tc.needle, h, tc.want)
		}
	}
	if locs := ViewLocations(ctrlPath, goCtrl, cat, strings.Index(goCtrl, `"pages/show.tmpl"`)+2); len(locs) < 2 {
		t.Errorf("expected app + package override locations, got %+v", locs)
	}
	if def := ViewDefinition(ctrlPath, goCtrl, cat, strings.Index(goCtrl, `"title":`)+2); def == nil {
		t.Error("expected ViewDefinition on data map key to jump to template")
	}
	if locs := ViewLocations(ctrlPath, goCtrl, cat, strings.Index(goCtrl, `"app_name"`)+2); len(locs) == 0 {
		t.Error("expected ViewLocations on Shared('app_name')")
	}
	if ds := ViewDiagnostics(ctrlPath, goCtrl, cat); !hasDiag(ds, "unknown_shared") || !hasDiag(ds, "pages/missing_new.tmpl") {
		t.Errorf("unexpected ViewDiagnostics: %+v", ds)
	}
	if acts := ViewCodeActions(ctrlPath, goCtrl, cat, strings.Index(goCtrl, `"pages/missing_new.tmpl"`)+2); len(acts) == 0 || !strings.Contains(acts[0].Title, "Create view template") {
		t.Errorf("expected scaffold template quickfix, got %+v", acts)
	}
	if acts := ViewCodeActions(ctrlPath, goCtrl, cat, strings.Index(goCtrl, `"title":`)); len(acts) == 0 || !strings.Contains(acts[0].Title, "Fill missing template variables") {
		t.Errorf("expected fill missing vars quickfix, got %+v", acts)
	}
	if hints := ViewInlayHints(ctrlPath, goCtrl, cat); len(hints) == 0 {
		t.Error("expected ViewInlayHints for package override / missing vars")
	}

	// 3. Template Diagnostics: typo var, unknown func, wrong arity, missing static asset, empty file, define mismatch
	tDiags := TemplateDiagnostics(showPath, showTmpl, cat)
	for _, want := range []string{
		"did you mean '.title'",
		"Unknown template function 'badFunc'",
		"expects at least 2 argument(s)",
		"Static asset '/css/missing.css' not found",
	} {
		if !hasDiag(tDiags, want) {
			t.Errorf("TemplateDiagnostics missing %q\ngot: %+v", want, tDiags)
		}
	}
	if hasDiag(tDiags, "/css/app.css") {
		t.Errorf("existing asset /css/app.css must not warn")
	}
	if ds := TemplateDiagnostics(emptyPath, "   ", cat); !hasDiag(ds, "Template file is empty") {
		t.Errorf("expected empty file hint, got %+v", ds)
	}
	if ds := TemplateDiagnostics(wrongDefPath, "{{ define \"wrong_name\" }}hi{{ end }}", cat); !hasDiag(ds, "does not match relative view path") {
		t.Errorf("expected define mismatch hint, got %+v", ds)
	}

	// 4. Template Completions: include, pipe, nested struct field, $local, scoped dot, HTML route & asset
	if items := TemplateCompletions(showPath, `{{ template "`, cat, len(`{{ template "`)); len(items) == 0 {
		t.Error("expected template include completions")
	}
	if items := TemplateCompletions(showPath, `{{ .title | `, cat, len(`{{ .title | `)); len(items) == 0 {
		t.Error("expected pipe FuncMap/builtin completions")
	}
	if items := TemplateCompletions(showPath, `{{ .user.Em`, cat, len(`{{ .user.Em`)); len(items) == 0 {
		t.Errorf("expected nested struct field completions for .user.")
	}
	if items := TemplateCompletions(showPath, showTmpl, cat, strings.Index(showTmpl, "$u.Email")+1); len(items) == 0 {
		t.Error("expected local $ variable completions")
	}
	if items := TemplateCompletions(showPath, showTmpl, cat, strings.Index(showTmpl, "- {{ .Email }}")+6); len(items) == 0 {
		t.Error("expected scoped dot completions inside range")
	}
	if items := TemplateCompletions(showPath, `<a href="/`, cat, len(`<a href="/`)); len(items) == 0 {
		t.Error("expected HTML route completions")
	}
	if items := TemplateCompletions(showPath, `<img src="/`, cat, len(`<img src="/`)); len(items) == 0 {
		t.Error("expected HTML asset completions")
	}

	// 5. Template Hover & Definition across all constructs
	tmplHoverChecks := []struct {
		needle string
		delta  int
		want   string
	}{
		{`"pages/show.tmpl"`, 2, "Template Definition `pages/show.tmpl`"},
		{`"partials/nav.tmpl"`, 2, "Included Template `partials/nav.tmpl`"},
		{`.user.Email`, 7, "models.User.Email"},
		{`$u.Email`, 1, "Local Template Variable `$u`"},
		{`.csrf_token`, 3, "CSRF token"},
		{`{{ .title | upper }}`, 5, "Template Variable `.title`"},
		{`{{ .title | upper }}`, 13, "Custom Template Function `upper`"},
		{`{{ $.title }}`, 5, "root context"},
		{`<em>{{ .Email }}</em>`, 9, "Scoped Field `.Email`"},
		{`"partials/nav.tmpl" .`, 20, "Template Context `.` passed to `partials/nav.tmpl`"},
		{`href="/pages"`, 8, "Goravel Route `/pages`"},
		{`href="/css/app.css"`, 8, "Static Asset `/css/app.css`"},
	}
	for _, tc := range tmplHoverChecks {
		off := strings.Index(showTmpl, tc.needle) + tc.delta
		h := TemplateHover(showPath, showTmpl, cat, off)
		if h == nil || !strings.Contains(h.Markdown, tc.want) {
			t.Errorf("TemplateHover(%q) = %+v want %q", tc.needle, h, tc.want)
		}
	}
	for _, tc := range []struct {
		needle string
		delta  int
	}{
		{`"pages/show.tmpl"`, 2},
		{`"partials/nav.tmpl"`, 2},
		{`.user.Email`, 7},
		{`$u.Email`, 2},
		{`.title`, 2},
		{`upper`, 2},
		{`href="/pages"`, 8},
		{`href="/css/app.css"`, 8},
	} {
		if locs := TemplateDefinition(showPath, showTmpl, cat, strings.Index(showTmpl, tc.needle)+tc.delta); len(locs) == 0 {
			t.Errorf("TemplateDefinition(%q) returned empty", tc.needle)
		}
	}

	// 6. Template CodeActions, CodeLenses, DocumentSymbols, InlayHints, Target/Spans
	if acts := TemplateCodeActions(emptyPath, "   ", cat, 0); len(acts) == 0 || !strings.Contains(acts[0].Title, "Scaffold") {
		t.Errorf("expected scaffold empty template action, got %+v", acts)
	}
	if acts := TemplateCodeActions(wrongDefPath, "{{ define \"wrong_name\" }}hi{{ end }}", cat, 12); len(acts) == 0 || !strings.Contains(acts[0].Title, "Sync {{ define }}") {
		t.Errorf("expected sync define name action, got %+v", acts)
	}
	if acts := TemplateCodeActions(showPath, `{{ template "partials/new.tmpl" . }}`, cat, 15); len(acts) == 0 || !strings.Contains(acts[0].Title, "Create partial template") {
		t.Errorf("expected create partial action, got %+v", acts)
	}
	if acts := TemplateCodeActions(showPath, showTmpl, cat, strings.Index(showTmpl, "{{ .titl }}")+5); len(acts) < 2 {
		t.Errorf("expected typo fix + add to controller map actions, got %+v", acts)
	}
	if lenses := TemplateCodeLenses(showPath, cat); len(lenses) < 2 {
		t.Errorf("expected caller + package override lenses on show.tmpl, got %+v", lenses)
	}
	if lenses := TemplateCodeLenses(navPath, cat); len(lenses) == 0 || !strings.Contains(lenses[0].Title, "Included by") {
		t.Errorf("expected Included by lens on nav.tmpl, got %+v", lenses)
	}
	if lenses := TemplateCodeLenses(wrongDefPath, cat); len(lenses) == 0 || !strings.Contains(lenses[0].Title, "Unreferenced view") {
		t.Errorf("expected unreferenced lens on orphan view, got %+v", lenses)
	}
	if syms := TemplateDocumentSymbols(showPath, showTmpl); len(syms) < 4 {
		t.Errorf("expected >= 4 template document symbols, got %+v", syms)
	}
	if hints := TemplateInlayHints(showPath, showTmpl, cat); len(hints) == 0 {
		t.Errorf("expected template inlay hints for typed local vars")
	}
	tv, vk, isShared := TemplateTargetAtOffset(showPath, showTmpl, cat, strings.Index(showTmpl, ".title")+2)
	if tv != "pages/show.tmpl" || vk != "title" || isShared {
		t.Errorf("unexpected TemplateTargetAtOffset: (%q, %q, %v)", tv, vk, isShared)
	}
	if spans := FindViewAndTemplateSpans(showPath, showTmpl, cat, "pages/show.tmpl", "title", false); len(spans) < 2 {
		t.Errorf("expected >= 2 spans for .title in show.tmpl, got %+v", spans)
	}
	if spans := FindViewAndTemplateSpans(ctrlPath, goCtrl, cat, "pages/show.tmpl", "title", false); len(spans) != 1 {
		t.Errorf("expected 1 data map key span for title in controller, got %+v", spans)
	}
}
