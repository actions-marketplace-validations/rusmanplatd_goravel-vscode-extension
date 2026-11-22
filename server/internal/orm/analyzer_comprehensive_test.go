package orm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

func buildRichORMProject(t *testing.T) (string, *indexer.Catalog) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		".env": "APP_NAME=GoravelApp\nJWT_SECRET=supersecret\n",
		"config/app.go": `package config
func init() {
	c := facades.Config()
	c.Add("app", map[string]any{
		"name": c.Env("APP_NAME", "DefaultApp"),
	})
}
`,
		"lang/en.json": `{"messages": {"welcome": "Welcome back, user!"}}`,
		"lang/id.json": `{"messages": {"welcome": "Selamat datang!"}}`,
		"app/providers/auth.go": `package providers
func Boot() {
	facades.Gate().Define("update-city", func() {})
}
`,
		"app/rules/custom_code.go": `package rules
type CustomCode struct{}
func (r *CustomCode) Signature() string { return "custom_code" }
func (r *CustomCode) Message() string { return "bad code" }
`,
		"app/models/country.go": `package models
type Country struct {
	ID   uint   ` + "`gorm:\"column:id\"`" + `
	Name string ` + "`gorm:\"column:name\"`" + `
}
func (Country) TableName() string { return "countries" }
`,
		"app/models/province.go": `package models
type Province struct {
	ID        uint     ` + "`gorm:\"column:id\"`" + `
	CountryID uint     ` + "`gorm:\"column:country_id\"`" + `
	Name      string   ` + "`gorm:\"column:name\"`" + `
	Country   *Country ` + "`gorm:\"foreignKey:CountryID;references:ID\"`" + `
}
func (Province) TableName() string { return "provinces" }
`,
		"app/models/city.go": `package models
const CityOriginalTableName = "cities"
type City struct {
	ID         uint      ` + "`gorm:\"column:id\"`" + `
	ProvinceID uint      ` + "`gorm:\"column:province_id\"`" + `
	Name       string    ` + "`gorm:\"column:name\"`" + `
	Code       *string   ` + "`gorm:\"column:code\"`" + `
	Province   *Province ` + "`gorm:\"foreignKey:ProvinceID;references:ID\"`" + `
}
func (City) TableName() string { return CityOriginalTableName }
`,
		"app/http/requests/update_city_request.go": `package requests
type UpdateCityRequest struct {
	Name string ` + "`json:\"name\"`" + `
	Code string ` + "`json:\"code\"`" + `
}
`,
		"app/http/controllers/city_controller.go": `package controllers
type CityController struct{}
func (c *CityController) Update(ctx http.Context) {}
`,
		"routes/api.go": `package routes
func Api() {
	ctrl := controllers.NewCityController()
	facades.Route().Put("/cities/{id}", ctrl.Update)
}
`,
	}
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

func TestORM_QueryChainsJoinsRelationsPatchAndSQLFiltering(t *testing.T) {
	_, cat := buildRichORMProject(t)

	src := `package repositories

type CityRepo struct{}

func (r *CityRepo) tableName() string {
	return (models.City{}).TableName()
}

func (r *CityRepo) Queries(ctx http.Context) {
	var city models.City
	// Chained Table alias + LeftJoin + qualified columns + PostgreSQL cast + escaped quote
	_ = facades.DB().Table("cities ct").
		LeftJoin("provinces p ON p.id = ct.province_id").
		Select("ct.id", "p.name AS prov_name", "p.bad_join_col").
		Where("ct.name = 'O''Brien' AND ct.id::text = ? AND p.ghost_p = ?", "1", "2").
		Order("ct.name DESC, p.bad_order ASC").
		Get(&city)

	// Concat table var with alias + Join unknown table
	tbl := (models.City{}).TableName()
	_ = facades.DB().Table(tbl + " AS c2").
		Join("unknown_tbl u ON u.id = c2.id").
		Where("c2.name = ?", "x").
		Get(nil)

	// Nested With relation + callback rebinding + invalid nested relation
	_ = facades.Orm().Query().Model(&models.City{}).
		With("Province.Country").
		With("Province", func(pq orm.Query) orm.Query {
			return pq.Where("country_id = ?", 1).Where("bad_rel_col = ?", 2)
		}).
		With("Province.MissingRel").
		Update("code", "BDG")

	// Updates map + BuildPatchQuery with map var & index assignments
	_ = facades.Orm().Query().Model(&models.City{}).Updates(map[string]any{"name": "X", "bad_upd": 1})
	patches := map[string]any{"name": "Y", "bad_patch": 2}
	patches["bad_idx_patch"] = 3
	_ = BuildPatchQuery(CityOriginalTableName, 1, patches)

	// Receiver defaultTable fallback on query var
	q := facades.Orm().Query()
	_ = q.Where("bad_default_col = ?", 1).Pluck("name", nil)

	// Non-SQL spans to verify SQLDiagnostics filters them out
	_ = facades.Config().GetString("app.missing_key")
	_ = facades.Config().Get("app")
	_ = facades.Config().Env("MISSING_ENV")
	_ = facades.Lang(ctx).Get("messages.missing")
	_ = facades.Gate().Allows("missing-gate")
	_ = facades.Gate().Any([]string{"update-city", "bad-gate"})
}
`
	fa := AnalyzeFile("/proj/app/repositories/city_repo.go", []byte(src), cat)

	allDiags := fa.Diagnostics(cat)
	sqlDiags := fa.SQLDiagnostics(cat)

	var allMsgs, sqlMsgs []string
	for _, d := range allDiags {
		allMsgs = append(allMsgs, d.Message)
	}
	for _, d := range sqlDiags {
		sqlMsgs = append(sqlMsgs, d.Message)
	}
	allJoined := strings.Join(allMsgs, " | ")
	sqlJoined := strings.Join(sqlMsgs, " | ")

	for _, wantSQL := range []string{
		"Column 'bad_join_col' does not exist on table 'provinces'",
		"Column 'ghost_p' does not exist on table 'provinces'",
		"Column 'bad_order' does not exist on table 'provinces'",
		"Table 'unknown_tbl' not found in schema",
		"Column 'bad_rel_col' does not exist on table 'provinces'",
		"Relation 'MissingRel' is not defined on model 'Province'",
		"Column 'bad_upd' does not exist on table 'cities'",
		"Column 'bad_patch' does not exist on table 'cities'",
		"Column 'bad_idx_patch' does not exist on table 'cities'",
		"Column 'bad_default_col' does not exist on table 'cities'",
	} {
		if !strings.Contains(sqlJoined, wantSQL) {
			t.Errorf("SQLDiagnostics missing %q\ngot: %v", wantSQL, sqlMsgs)
		}
	}

	// Non-SQL diagnostics appear in Diagnostics() but NOT in SQLDiagnostics()
	for _, wantNonSQL := range []string{
		"Unknown config key 'app.missing_key'",
		"Environment variable 'MISSING_ENV' is not defined",
		"Missing translation key 'messages.missing'",
		"Unknown Gate ability 'missing-gate'",
		"Unknown Gate ability 'bad-gate'",
	} {
		if !strings.Contains(allJoined, wantNonSQL) {
			t.Errorf("Diagnostics missing %q\ngot: %v", wantNonSQL, allMsgs)
		}
		if strings.Contains(sqlJoined, wantNonSQL) {
			t.Errorf("SQLDiagnostics must NOT include %q", wantNonSQL)
		}
	}
	// Namespace prefix config access `Get("app")` must not warn
	if strings.Contains(allJoined, "'app'") {
		t.Errorf("namespace config key 'app' must not warn: %v", allMsgs)
	}

	// SemanticTokens on ORM spans
	toks := fa.SemanticTokens()
	if len(toks) == 0 {
		t.Error("expected semantic tokens for ORM spans")
	}
}

func TestORM_ValidationRulesControllerRoutesSwaggerCompletionHoverDefHints(t *testing.T) {
	_, cat := buildRichORMProject(t)

	reqSrc := `package requests
type UpdateCityRequest struct{}
func (r *UpdateCityRequest) Rules(ctx http.Context) map[string]string {
	return map[string]string{
		"name": "required|custom_code|exists:cities,name|unique:provinces,bad_col|exists:ghost_tbl,id|not_a_rule",
	}
}
`
	faReq := AnalyzeFile("/proj/app/http/requests/update_city_request.go", []byte(reqSrc), cat)
	var rMsgs []string
	for _, d := range faReq.Diagnostics(cat) {
		rMsgs = append(rMsgs, d.Message)
	}
	rJoined := strings.Join(rMsgs, " | ")
	for _, want := range []string{
		"Column 'bad_col' does not exist on table 'provinces'",
		"Table 'ghost_tbl' not found in schema for 'exists' rule",
		"Unknown validation rule 'not_a_rule'",
	} {
		if !strings.Contains(rJoined, want) {
			t.Errorf("request rules diagnostics missing %q, got %v", want, rMsgs)
		}
	}

	// Rule field completion, hover, definition
	if items, ok := faReq.Completion(strings.Index(reqSrc, `"name"`)+2, cat); !ok || len(items) < 2 {
		t.Errorf("expected rule field completions, ok=%v items=%v", ok, items)
	}
	if md, _, _, ok := faReq.Hover(strings.Index(reqSrc, `"name"`)+2, cat); !ok || !strings.Contains(md, "UpdateCityRequest.Name") {
		t.Errorf("expected hover on rule field, ok=%v md=%q", ok, md)
	}
	if locs := faReq.Definition(strings.Index(reqSrc, `"name"`)+2, cat); len(locs) == 0 {
		t.Error("expected definition for rule field")
	}

	// Rule value: exists:table,col hover, definition, completion + custom rule hover & definition
	if md, _, _, ok := faReq.Hover(strings.Index(reqSrc, "exists:cities,name")+15, cat); !ok || !strings.Contains(md, "cities.name") {
		t.Errorf("expected column hover inside exists rule, ok=%v md=%q", ok, md)
	}
	if md, _, _, ok := faReq.Hover(strings.Index(reqSrc, "exists:cities")+9, cat); !ok || !strings.Contains(md, "Table `cities`") {
		t.Errorf("expected table hover inside exists rule, ok=%v md=%q", ok, md)
	}
	if locs := faReq.Definition(strings.Index(reqSrc, "exists:cities,name")+15, cat); len(locs) == 0 {
		t.Error("expected column definition inside exists rule")
	}
	if locs := faReq.Definition(strings.Index(reqSrc, "custom_code")+2, cat); len(locs) == 0 {
		t.Error("expected definition for custom_code rule")
	}
	if md, _, _, ok := faReq.Hover(strings.Index(reqSrc, "custom_code")+2, cat); !ok || !strings.Contains(md, "bad code") {
		t.Errorf("expected hover on custom_code rule, ok=%v md=%q", ok, md)
	}
	if items, ok := faReq.Completion(strings.Index(reqSrc, "exists:cities,")+len("exists:cities,"), cat); !ok || len(items) == 0 {
		t.Errorf("expected column completion inside exists:cities,, ok=%v", ok)
	}
	if items, ok := faReq.Completion(strings.Index(reqSrc, "exists:")+len("exists:"), cat); !ok || len(items) == 0 {
		t.Errorf("expected table completion inside exists:, ok=%v", ok)
	}

	// Controller with Swagger mismatch, Route param, Request input, Config, Env, Lang, Gate, Validation().Make
	ctrlSrc := `package controllers
type CityController struct{}
// @Router /wrong/{id} [put]
func (c *CityController) Update(ctx http.Context) {
	var req requests.UpdateCityRequest
	_ = req
	_ = ctx.Request().Route("id")
	_ = ctx.Request().Route("missing_param")
	_ = ctx.Request().Input("name")
	_ = ctx.Request().Validate(map[string]string{"name": "required"})
	_ = facades.Validation().Make(nil, map[string]string{"code": "string"})
	_ = facades.Config().GetString("app.name")
	_ = facades.Config().Env("APP_NAME")
	_ = facades.Lang(ctx).Get("messages.welcome")
	_ = facades.Gate().Allows("update-city")
	_ = facades.DB().Table("cities ct").LeftJoin("provinces p ON p.id = ct.province_id").With("Province.Country").Where("p.name = ?", "x").Order("ct.name DESC").Get(nil)
}
`
	faCtrl := AnalyzeFile("/proj/app/http/controllers/city_controller.go", []byte(ctrlSrc), cat)
	var cMsgs []string
	for _, d := range faCtrl.Diagnostics(cat) {
		cMsgs = append(cMsgs, d.Message)
	}
	cJoined := strings.Join(cMsgs, " | ")
	if !strings.Contains(cJoined, "@Router /wrong/{id} [put] does not match registered route PUT /cities/{id}") {
		t.Errorf("expected Swagger mismatch diagnostic, got %v", cMsgs)
	}
	if !strings.Contains(cJoined, "Route parameter 'missing_param' is not defined") {
		t.Errorf("expected missing route param diagnostic, got %v", cMsgs)
	}

	// Hover + Completion + Definition across span kinds
	checks := []struct {
		needle string
		delta  int
		hover  string
		wantDef bool
	}{
		{`Route("id")`, 8, "Route Parameter `id`", true},
		{`Input("name")`, 8, "UpdateCityRequest.Name", true},
		{`"app.name"`, 3, "Config `app.name`", true},
		{`"APP_NAME"`, 3, "Env `APP_NAME`", false},
		{`"messages.welcome"`, 3, "Welcome back", true},
		{`"update-city"`, 3, "Gate Ability `update-city`", true},
		{`"provinces p ON p.id = ct.province_id"`, 3, "Table `provinces`", true},
		{`p.id = ct.province_id`, 3, "provinces.id", true},
		{`"Province.Country"`, 2, "Relation `Province`", true},
		{`"Province.Country"`, 11, "Relation `Country`", true},
		{`p.name = ?`, 3, "provinces.name", true},
	}
	for _, ch := range checks {
		off := strings.Index(ctrlSrc, ch.needle) + ch.delta
		md, _, _, ok := faCtrl.Hover(off, cat)
		if !ok || !strings.Contains(md, ch.hover) {
			t.Errorf("Hover(%q): ok=%v md=%q want %q", ch.needle, ok, md, ch.hover)
		}
		if ch.wantDef {
			if locs := faCtrl.Definition(off, cat); len(locs) == 0 {
				t.Errorf("Definition(%q): expected at least 1 location", ch.needle)
			}
		}
		if items, ok := faCtrl.Completion(off, cat); !ok || len(items) == 0 {
			t.Errorf("Completion(%q): expected items, ok=%v len=%d", ch.needle, ok, len(items))
		}
	}

	// InlayHints for Config and Lang
	hints := faCtrl.InlayHints(cat)
	if len(hints) < 2 {
		t.Fatalf("expected >= 2 inlay hints (config + lang), got %+v", hints)
	}
	joinedHints := hints[0].Label + " " + hints[1].Label
	if !strings.Contains(joinedHints, "APP_NAME") || !strings.Contains(joinedHints, "Welcome back") {
		t.Errorf("unexpected inlay hints: %+v", hints)
	}
}
