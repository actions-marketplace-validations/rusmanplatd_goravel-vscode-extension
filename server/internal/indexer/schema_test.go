package indexer

import "testing"

func TestParseAndResolveSQLSource(t *testing.T) {
	cases := []struct {
		in   string
		want SQLSource
		ok   bool
	}{
		{"", SQLSourceAuto, true},
		{"auto", SQLSourceAuto, true},
		{"model", SQLSourceModel, true},
		{"models", SQLSourceModel, true},
		{"live-db", SQLSourceLiveDB, true},
		{"livedb", SQLSourceLiveDB, true},
		{"db", SQLSourceLiveDB, true},
		{"both", SQLSourceBoth, true},
		{"invalid", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseSQLSource(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseSQLSource(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}

	if got := ResolveSQLSource(SQLSourceAuto, false); got != SQLSourceModel {
		t.Errorf("ResolveSQLSource(auto, false) = %q, want model", got)
	}
	if got := ResolveSQLSource(SQLSourceAuto, true); got != SQLSourceBoth {
		t.Errorf("ResolveSQLSource(auto, true) = %q, want both", got)
	}
	if got := ResolveSQLSource(SQLSourceLiveDB, false); got != SQLSourceLiveDB {
		t.Errorf("ResolveSQLSource(live-db, false) = %q, want live-db", got)
	}
}

func TestCatalogForSQL(t *testing.T) {
	c := NewCatalog()
	c.LiveDBConnected = true

	users := c.ensure("users")
	users.Model = "User"
	users.FromModel = true
	users.FromLiveDB = true
	users.add(&Column{Name: "id", GoName: "ID", FromModel: true, FromLiveDB: true})
	users.add(&Column{Name: "model_only_col", GoName: "ModelOnlyCol", FromModel: true})
	users.add(&Column{Name: "db_only_col", SQLType: "TEXT", FromLiveDB: true})
	c.Models["User"] = users

	modelOnlyTbl := c.ensure("draft_items")
	modelOnlyTbl.Model = "DraftItem"
	modelOnlyTbl.FromModel = true
	modelOnlyTbl.add(&Column{Name: "id", FromModel: true})
	c.Models["DraftItem"] = modelOnlyTbl

	liveOnlyTbl := c.ensure("legacy_audit")
	liveOnlyTbl.FromLiveDB = true
	liveOnlyTbl.add(&Column{Name: "id", FromLiveDB: true})

	modelCat := c.ForSQL(SQLSourceModel)
	if modelCat.Table("draft_items") == nil {
		t.Fatalf("expected draft_items in model source")
	}
	if modelCat.Table("legacy_audit") != nil {
		t.Fatalf("did not expect legacy_audit in model source")
	}
	if u := modelCat.Table("users"); u == nil || u.Column("model_only_col") == nil || u.Column("db_only_col") != nil {
		t.Fatalf("unexpected users columns in model source: %+v", u)
	}

	liveCat := c.ForSQL(SQLSourceLiveDB)
	if liveCat.Table("legacy_audit") == nil {
		t.Fatalf("expected legacy_audit in live-db source")
	}
	if liveCat.Table("draft_items") != nil {
		t.Fatalf("did not expect draft_items in live-db source")
	}
	if u := liveCat.Table("users"); u == nil || u.Column("db_only_col") == nil || u.Column("model_only_col") != nil || u.Model != "User" {
		t.Fatalf("unexpected users in live-db source: %+v", u)
	}

	bothCat := c.ForSQL(SQLSourceBoth)
	if bothCat.Table("draft_items") == nil || bothCat.Table("legacy_audit") == nil {
		t.Fatalf("expected both tables in both source")
	}
	if u := bothCat.Table("users"); u == nil || u.Column("db_only_col") == nil || u.Column("model_only_col") == nil {
		t.Fatalf("expected merged columns in both source")
	}
}

func TestResolveDBConfigFromOSEnv(t *testing.T) {
	t.Setenv("DB_CONNECTION", "postgres")
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_DATABASE", "ci_db")
	t.Setenv("DB_USERNAME", "ci_user")
	t.Setenv("DB_PASSWORD", "ci_pass")

	c := NewCatalog()
	cfg := ResolveDBConfig(c)
	if cfg.Driver != "postgres" || cfg.Host != "127.0.0.1" || cfg.Port != "5432" || cfg.Database != "ci_db" || cfg.Username != "ci_user" || cfg.Password != "ci_pass" {
		t.Fatalf("unexpected resolved DB config from OS env: %+v", cfg)
	}
}

