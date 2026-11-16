package indexer

import "testing"

const fixture = "../../testdata/project"

func build(t *testing.T) *Catalog {
	t.Helper()
	return Build(fixture)
}

// ---------- positive ----------

func TestCatalog_Positive_TablesIndexedFromMigrationsAndModels(t *testing.T) {
	cat := build(t)
	for _, name := range []string{"ref_geo_cities", "ref_geo_provinces"} {
		if cat.Table(name) == nil {
			t.Fatalf("expected table %q in catalog, have %v", name, cat.TableNames())
		}
	}
	cols := map[string]bool{}
	for _, c := range cat.Table("ref_geo_cities").Columns {
		cols[c.Name] = true
	}
	for _, want := range []string{"id", "province_id", "name", "code", "latitude", "longitude", "deleted_at"} {
		if !cols[want] {
			t.Errorf("ref_geo_cities missing column %q", want)
		}
	}
}

func TestCatalog_Positive_ModelAndSchemaLookup(t *testing.T) {
	cat := build(t)
	if cat.Model("RefGeoCity") == nil {
		t.Error("expected model RefGeoCity")
	}
	if StripSchema("geo_app.ref_geo_cities") != "ref_geo_cities" {
		t.Error("StripSchema should drop the schema prefix")
	}
	if StripSchema("ref_geo_cities") != "ref_geo_cities" {
		t.Error("StripSchema should keep unqualified names")
	}
}

func TestCatalog_Positive_ConfigKeys(t *testing.T) {
	cat := build(t)
	for _, k := range []string{"jwt.secret", "jwt.ttl"} {
		if cat.Config(k) == nil {
			t.Errorf("expected config key %q, have %v", k, cat.ConfigKeys())
		}
	}
}

func TestCatalog_Positive_RoutesAndControllers(t *testing.T) {
	cat := build(t)
	rs := cat.RoutesForHandler("CityController", "Index")
	if len(rs) != 1 {
		t.Fatalf("expected 1 route for CityController.Index, got %d", len(rs))
	}
	if rs[0].Path != "/api/v1/geo/cities" {
		t.Errorf("unexpected route path %q", rs[0].Path)
	}
}

func TestCatalog_Positive_CustomRules(t *testing.T) {
	cat := build(t)
	if cat.Rule("alpha_dash_custom") == nil {
		t.Errorf("expected custom rule alpha_dash_custom, have %v", cat.RuleNames())
	}
}

func TestCatalog_Positive_RequestStructs(t *testing.T) {
	cat := build(t)
	st := cat.Struct("CreateCityRequest")
	if st == nil {
		t.Fatal("expected struct CreateCityRequest")
	}
	if st.Has("name") == nil || st.Has("province_id") == nil {
		t.Error("expected json-tag lookup for name and province_id")
	}
}

func TestSnake_Positive(t *testing.T) {
	cases := map[string]string{
		"ProvinceID":  "province_id",
		"Name":        "name",
		"CreatedByID": "created_by_id",
		"RefGeoCity":  "ref_geo_city",
	}
	for in, want := range cases {
		if got := Snake(in); got != want {
			t.Errorf("Snake(%q)=%q want %q", in, got, want)
		}
	}
}

// ---------- negative ----------

func TestCatalog_Negative_UnknownLookupsReturnNil(t *testing.T) {
	cat := build(t)
	if cat.Table("does_not_exist") != nil {
		t.Error("unknown table must be nil")
	}
	if cat.Model("Nope") != nil {
		t.Error("unknown model must be nil")
	}
	if cat.Config("jwt.missing") != nil {
		t.Error("unknown config key must be nil")
	}
	if cat.Rule("not_a_rule") != nil {
		t.Error("unknown rule must be nil")
	}
	if cat.Struct("Ghost") != nil {
		t.Error("unknown struct must be nil")
	}
}

func TestCatalog_Negative_UnknownHandlerHasNoRoutes(t *testing.T) {
	cat := build(t)
	if rs := cat.RoutesForHandler("CityController", "Destroy"); len(rs) != 0 {
		t.Errorf("unexpected routes for unregistered action: %v", rs)
	}
	if rs := cat.RoutesForHandler("GhostController", "Index"); len(rs) != 0 {
		t.Errorf("unexpected routes for unknown controller: %v", rs)
	}
}

func TestCatalog_Negative_MissingRootDoesNotPanic(t *testing.T) {
	cat := Build("/nonexistent/path/for/goravel")
	if cat == nil {
		t.Fatal("Build must always return a catalog")
	}
	if len(cat.Tables) != 0 || len(cat.Routes) != 0 {
		t.Error("missing root should yield an empty catalog")
	}
}

func TestCatalog_Negative_EmptyCatalogLookups(t *testing.T) {
	cat := NewCatalog()
	if cat.Table("x") != nil || cat.Config("x") != nil || cat.Rule("x") != nil {
		t.Error("empty catalog lookups must return nil")
	}
	if len(cat.TableNames()) != 0 {
		t.Error("empty catalog has no tables")
	}
}

func TestParseSQLSource_Negative(t *testing.T) {
	if _, ok := ParseSQLSource("bogus-source"); ok {
		t.Error("invalid SQL source must be rejected")
	}
	if _, ok := ParseSQLSource("model; DROP"); ok {
		t.Error("injection-like SQL source must be rejected")
	}
}

func TestParseSQLSource_Positive(t *testing.T) {
	for _, in := range []string{"", "auto", " MODEL ", "live-db", "both"} {
		if _, ok := ParseSQLSource(in); !ok {
			t.Errorf("ParseSQLSource(%q) should be accepted", in)
		}
	}
}

func TestSnake_Negative_EmptyInput(t *testing.T) {
	if Snake("") != "" {
		t.Error("Snake(\"\") must be empty")
	}
}
