package indexer

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func makeProject(t *testing.T, files map[string]string) string {
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
	return root
}

func TestIndexer_ModelsAndRelations(t *testing.T) {
	root := makeProject(t, map[string]string{
		"app/models/base.go": `package models
const (
	OrderOriginalTableName = "orders"
	OrderOriginalSchemaName = "sales"
)
type AuditFields struct {
	TenantID string ` + "`db:\"tenant_id\"`" + `
}
`,
		"app/models/customer.go": `package models
type Customer struct {
	orm.Model
	orm.SoftDeletes
	// @Description Customer full name
	// @Example "Jane Doe"
	FullName string ` + "`gorm:\"column:full_name\" json:\"full_name\"`" + `
	Secret   string ` + "`json:\"-\"`" + `
	Ignored  string ` + "`gorm:\"-\"`" + `
	SkipMig  string ` + "`gorm:\"-:migration\"`" + `
	Legacy   string ` + "`db:\"legacy_code,omitempty\"`" + `
	internal string
}
func (Customer) TableName() string { return "customers" }
func (Customer) OriginalSchemaName() string { return "crm" }
`,
		"app/models/order.go": `package models
type Order struct {
	AuditFields
	orm.Timestamps
	ID         uint      ` + "`gorm:\"primaryKey\"`" + `
	CustomerID uint
	Customer   *Customer ` + "`gorm:\"foreignKey:CustomerID;references:ID\"`" + `
	Items      []OrderItem
	Tags       []Tag     ` + "`gorm:\"many2many:order_tags\"`" + `
}
func (Order) TableName() string { return OrderOriginalTableName }
func (Order) OriginalSchemaName() string { return OrderOriginalSchemaName }
`,
		"app/models/order_item.go": `package models
type OrderItem struct {
	ID      uint
	OrderID uint
	SKU     string
}
func (OrderItem) TableName() string { return "order_items" }
`,
		"app/models/tag.go": `package models
type Tag struct {
	ID   uint
	Name string
}
func (Tag) TableName() string { return "tags" }
`,
		"app/models/broken.go": "package models\ntype Broken struct {",
		"app/services/order_service.go": `package services
type OrderSummary struct {
	Total int ` + "`json:\"total\"`" + `
}
type OrderService struct{}
func (s *OrderService) GetSummary() *OrderSummary { return nil }
func ListOrders() []Order { return nil }
func Ping() string { return "ok" }
`,
	})

	cat := Build(root)

	// Table constants & schemas
	tc := cat.TableConsts["OrderOriginalTableName"]
	if tc == nil || tc.TableName != "orders" || tc.Model != "Order" {
		t.Fatalf("unexpected TableConst: %+v", tc)
	}
	cust := cat.Table("customers")
	if cust == nil || cust.Schema != "crm" || cust.Model != "Customer" {
		t.Fatalf("unexpected customers table: %+v", cust)
	}
	for _, want := range []string{"id", "created_at", "updated_at", "deleted_at", "full_name", "legacy_code"} {
		if cust.Column(want) == nil {
			t.Errorf("customers missing column %q", want)
		}
	}
	for _, skip := range []string{"secret", "ignored", "skip_mig", "internal"} {
		if cust.Column(skip) != nil {
			t.Errorf("customers should not include skipped field %q", skip)
		}
	}
	if fn := cust.Column("full_name"); fn == nil || fn.Doc != "Customer full name" || fn.Example != `"Jane Doe"` {
		t.Errorf("unexpected doc/example on full_name: %+v", fn)
	}

	// Embedded struct + explicit & implicit relations
	ord := cat.Model("Order")
	if ord == nil || ord.Schema != "sales" {
		t.Fatalf("unexpected Order table: %+v", ord)
	}
	if ord.Column("tenant_id") == nil || ord.Column("created_at") == nil {
		t.Errorf("expected embedded AuditFields & orm.Timestamps columns on Order, got %+v", ord.Columns)
	}
	if rel := ord.Relation("Customer"); rel == nil || rel.TargetTable != "customers" || rel.ForeignKey != "customer_id" || rel.References != "id" {
		t.Errorf("unexpected Customer relation: %+v", rel)
	}
	if col := ord.Column("customer_id"); col == nil || col.ForeignKey != "customers.id" {
		t.Errorf("expected FK customers.id on customer_id, got %+v", col)
	}
	if rel := ord.Relation("Items"); rel == nil || rel.TargetTable != "order_items" || rel.ForeignKey != "order_id" {
		t.Errorf("expected implicit HasMany Items relation, got %+v", rel)
	}
	// Implicit relation field Items should be removed from columns
	if ord.Column("items") != nil {
		t.Errorf("implicit relation field 'items' should not remain as a column")
	}
	if rel := ord.Relation("Tags"); rel == nil || rel.TargetTable != "tags" {
		t.Errorf("expected many2many Tags relation, got %+v", rel)
	}

	// Inferred *_id FK on OrderItem
	oi := cat.Table("order_items")
	if oi == nil || oi.Column("order_id") == nil || oi.Column("order_id").ForeignKey != "orders.id" {
		t.Errorf("expected inferred FK orders.id on order_items.order_id, got %+v", oi)
	}

	// indexRequests: Structs & FuncReturns
	if cat.Struct("OrderSummary") == nil {
		t.Error("expected OrderSummary struct indexed from app/services")
	}
	if cat.FuncReturns["OrderService.GetSummary"] != "OrderSummary" {
		t.Errorf("expected OrderService.GetSummary -> OrderSummary, got %q", cat.FuncReturns["OrderService.GetSummary"])
	}
	if cat.FuncReturns["ListOrders"] != "[]Order" {
		t.Errorf("expected ListOrders -> []Order, got %q", cat.FuncReturns["ListOrders"])
	}
	if _, ok := cat.FuncReturns["Ping"]; ok {
		t.Error("primitive string return should not be recorded in FuncReturns")
	}
}

func TestIndexer_MigrationsRawSQLAndBlueprints(t *testing.T) {
	root := makeProject(t, map[string]string{
		"database/migrations/20250101_01_create_accounts.go": `package migrations
type M20250101CreateAccounts struct{}
func (r *M20250101CreateAccounts) Signature() string { return "20250101_create_accounts" }
func (r *M20250101CreateAccounts) Up() error {
	return facades.Schema().Create("accounts", func(table schema.Blueprint) {
		table.ID()
		table.Uuid("external_id")
		table.Ulid("short_id")
		table.String("email")
		table.Char("country_code")
		table.Text("bio").Nullable()
		table.Integer("age")
		table.SmallInteger("rank")
		table.BigInteger("views")
		table.Boolean("active")
		table.Decimal("balance")
		table.Float("score")
		table.Date("birth_date")
		table.DateTime("last_login")
		table.Time("daily_at")
		table.Json("meta")
		table.Jsonb("settings")
		table.Binary("avatar")
		table.Enum("status")
		table.Morphs("owner")
		table.NullableMorphs("actor")
		table.ForeignId("org_id")
		table.Foreign("org_id").References("id").On("organizations")
		table.Unique("email")
		table.Index("org_id", "active")
		table.Timestamps()
		table.SoftDeletes()
	})
}
func (r *M20250101CreateAccounts) Down() error {
	return facades.Schema().DropIfExists("accounts")
}
`,
		"database/migrations/20250102_02_alter_accounts.go": `package migrations
func Up() error {
	return facades.Schema().Table("accounts", func(table schema.Blueprint) {
		table.RenameColumn("bio", "biography")
		table.DropColumn("rank", "score")
	})
}
func Down() error {
	return facades.Schema().Table("accounts", func(table schema.Blueprint) {
		table.DropColumn("email") // Down() must be ignored!
	})
}
`,
		"database/migrations_billing/20250103_03_invoices.go": `package migrations_billing
func Up() error {
	return facades.DB().Statement(` + "`" + `
		CREATE TABLE IF NOT EXISTS billing.invoices (
			id UUID PRIMARY KEY,
			account_id BIGINT NOT NULL REFERENCES accounts(id),
			amount NUMERIC(10,2) NOT NULL,
			old_note TEXT,
			temp_col VARCHAR(20),
			CONSTRAINT fk_acc FOREIGN KEY (account_id) REFERENCES accounts(id)
		);
		ALTER TABLE billing.invoices ADD COLUMN currency CHAR(3) NOT NULL;
		ALTER TABLE billing.invoices RENAME COLUMN old_note TO memo;
		ALTER TABLE billing.invoices DROP COLUMN temp_col;
		CREATE UNIQUE INDEX idx_inv_acc ON billing.invoices (account_id, currency);
	` + "`" + `)
}
func Down() error {
	return facades.DB().Statement("ALTER TABLE billing.invoices DROP COLUMN amount;")
}
`,
	})

	cat := Build(root)
	if len(cat.Migrations) != 3 {
		t.Fatalf("expected 3 migrations, got %d", len(cat.Migrations))
	}
	if cat.Migrations[0].Signature != "20250101_create_accounts" || cat.Migrations[0].Folder != "migrations" {
		t.Errorf("unexpected first migration metadata: %+v", cat.Migrations[0])
	}
	if cat.Migrations[2].Folder != "migrations_billing" || cat.Migrations[2].Table != "invoices" {
		t.Errorf("unexpected third migration metadata: %+v", cat.Migrations[2])
	}

	acc := cat.Table("accounts")
	if acc == nil {
		t.Fatal("expected accounts table")
	}
	wantCols := map[string]string{
		"id":           "BIGINT",
		"external_id":  "UUID",
		"short_id":     "CHAR(26)",
		"email":        "VARCHAR(255)",
		"country_code": "CHAR",
		"biography":    "TEXT",
		"age":          "INTEGER",
		"views":        "BIGINT",
		"active":       "BOOLEAN",
		"balance":      "DECIMAL",
		"birth_date":   "DATE",
		"last_login":   "TIMESTAMP",
		"daily_at":     "TIME",
		"meta":         "JSON",
		"settings":     "JSONB",
		"avatar":       "BYTEA",
		"status":       "VARCHAR(255)",
		"owner_id":     "VARCHAR(255)",
		"owner_type":   "VARCHAR(255)",
		"actor_id":     "VARCHAR(255)",
		"actor_type":   "VARCHAR(255)",
		"org_id":       "BIGINT",
		"created_at":   "TIMESTAMP",
		"updated_at":   "TIMESTAMP",
		"deleted_at":   "TIMESTAMP",
	}
	for col, wantType := range wantCols {
		c := acc.Column(col)
		if c == nil {
			t.Errorf("accounts missing column %q", col)
			continue
		}
		if c.SQLType != wantType {
			t.Errorf("accounts.%s SQLType=%q want %q", col, c.SQLType, wantType)
		}
	}
	// Dropped/renamed columns
	for _, gone := range []string{"bio", "rank", "score"} {
		if acc.Column(gone) != nil {
			t.Errorf("column %q should have been dropped/renamed", gone)
		}
	}
	// Nullability
	if !acc.Column("biography").Nullable || !acc.Column("actor_id").Nullable || acc.Column("owner_id").Nullable {
		t.Errorf("unexpected nullability on biography/actor_id/owner_id")
	}
	// FK & Indexes
	if acc.Column("org_id").ForeignKey != "organizations.id" {
		t.Errorf("expected org_id FK organizations.id, got %q", acc.Column("org_id").ForeignKey)
	}
	if !acc.HasIndex("id") || !acc.HasIndex("email") || !acc.HasIndex("org_id") || acc.HasIndex("active") {
		t.Errorf("unexpected HasIndex results on accounts: indexes=%+v", acc.Indexes)
	}

	// Raw SQL CREATE + ALTER + INDEX
	inv := cat.Table("invoices")
	if inv == nil {
		t.Fatal("expected invoices table")
	}
	if inv.Column("currency") == nil || inv.Column("memo") == nil || inv.Column("amount") == nil {
		t.Errorf("invoices missing expected columns: %+v", inv.Columns)
	}
	if inv.Column("old_note") != nil || inv.Column("temp_col") != nil {
		t.Errorf("invoices should not have dropped/renamed columns")
	}
	if inv.Column("account_id").ForeignKey != "accounts.id" {
		t.Errorf("expected invoices.account_id FK accounts.id, got %q", inv.Column("account_id").ForeignKey)
	}
	if !inv.HasIndex("account_id") || inv.HasIndex("currency") {
		t.Errorf("unexpected index on invoices: %+v", inv.Indexes)
	}
}

func TestIndexer_ConfigsAndEnv(t *testing.T) {
	root := makeProject(t, map[string]string{
		".env.example": "# comment\nAPP_NAME=Example\nDB_PORT=5432\nMALFORMED_LINE\n",
		".env":         "export APP_NAME=\"MyApp\"\nJWT_TTL='120'\n",
		"config/app.go": `package config
func init() {
	c := facades.Config()
	timeout := c.Env("APP_TIMEOUT", 30)
	c.Add("app", map[string]any{
		"name": c.Env("APP_NAME", "Default"),
		"timeout": timeout,
		"nested": map[string]any{
			"ttl": cast.ToInt(c.Env("JWT_TTL", 60)),
			"flag": true,
		},
		"helper": makeVal(),
	})
}
`,
	})
	cat := Build(root)
	if cat.EnvVars["APP_NAME"] != "MyApp" {
		t.Errorf(".env must override .env.example, got %q", cat.EnvVars["APP_NAME"])
	}
	if cat.EnvVars["DB_PORT"] != "5432" {
		t.Errorf("expected DB_PORT from .env.example, got %q", cat.EnvVars["DB_PORT"])
	}
	if ci := cat.Config("app.name"); ci == nil || ci.EnvKey != "APP_NAME" || ci.EnvVal != "MyApp" || ci.Default != `"Default"` {
		t.Errorf("unexpected app.name config: %+v", ci)
	}
	if ci := cat.Config("app.timeout"); ci == nil || ci.EnvKey != "APP_TIMEOUT" || ci.Default != "30" {
		t.Errorf("expected local var resolved for app.timeout, got %+v", ci)
	}
	if ci := cat.Config("app.nested.ttl"); ci == nil || ci.EnvKey != "JWT_TTL" || ci.EnvVal != "120" {
		t.Errorf("expected nested cast.ToInt(Env) resolved, got %+v", ci)
	}
	if ci := cat.Config("app.nested.flag"); ci == nil || ci.Default != "true" {
		t.Errorf("unexpected app.nested.flag: %+v", ci)
	}
	keys := cat.ConfigKeys()
	wantKeys := []string{"app.helper", "app.name", "app.nested.flag", "app.nested.ttl", "app.timeout"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Errorf("ConfigKeys=%v want %v", keys, wantKeys)
	}
}

func TestIndexer_HTTPRoutesAndGRPCRoutes(t *testing.T) {
	root := makeProject(t, map[string]string{
		"app/http/controllers/post_controller.go": `package controllers
type PostController struct{}
func (c *PostController) Index() {}
func (c *PostController) Store() {}
func (c *PostController) Show() {}
func (c *PostController) Update() {}
func (c *PostController) Destroy() {}
`,
		"routes/web.go": `package routes
func Web() {
	postCtrl := controllers.NewPostController()
	facades.Route().Prefix("/api/v1").Middleware(middleware.Auth(), middleware.CORS).Group(func(router route.Router) {
		router.Resource("/posts", postCtrl)
		router.Middleware(throttle).Get("/users/{id:[0-9]+}/profile/:tab", func(ctx http.Context) {
			_ = ctx.Response().View().Make("users/profile.tmpl", nil)
		})
		router.Post("/fallback", func(ctx http.Context) {
			_ = ctx.Response().View().First([]string{"a.tmpl", "b.tmpl"}, nil)
		})
		router.Any("/inline", (&controllers.PostController{}).Index)
	})
}
`,
		"proto/user.proto": `syntax = "proto3";
service UserService {
	rpc GetUser (GetUserRequest) returns (GetUserResponse);
	rpc StreamUsers (stream Empty) returns (stream UserReply);
}
service OrphanService {
	rpc Ping (PingReq) returns (PingRes);
}
`,
		"app/grpc/controllers/user_controller.go": `package controllers
type UserController struct{}
func (c *UserController) GetUser() {}
`,
		"app/grpc/controllers/orphan_controller.go": `package controllers
type OrphanController struct{}
func (c *OrphanController) Ping() {}
`,
		"routes/grpc.go": `package routes
func Grpc() {
	userCtrl := controllers.NewUserController()
	pb.RegisterUserServiceServer(facades.Grpc().Server(), userCtrl)
}
`,
	})

	cat := Build(root)
	// Resource creates 5 routes + 3 manual HTTP routes + 2 registered gRPC + 1 fallback gRPC = 11
	var httpCount, grpcCount int
	var foundClosureView, foundFallbackView, foundResourceShow, foundGrpcStream, foundOrphanGrpc bool
	for _, r := range cat.Routes {
		if r.Method == "gRPC" {
			grpcCount++
			if r.Path == "/UserService/StreamUsers" && r.Controller == "UserController" {
				foundGrpcStream = true
			}
			if r.Path == "/OrphanService/Ping" && r.Controller == "OrphanController" {
				foundOrphanGrpc = true
			}
			continue
		}
		httpCount++
		if r.Method == "GET" && r.Path == "/api/v1/posts/{id}" && r.Action == "Show" {
			foundResourceShow = true
			if !reflect.DeepEqual(r.Params, []string{"id"}) || !reflect.DeepEqual(r.Middlewares, []string{"Auth", "CORS"}) {
				t.Errorf("unexpected resource route metadata: %+v", r)
			}
			if !r.CtrlLoc.Valid() {
				t.Errorf("expected controller location resolved for PostController.Show")
			}
		}
		if r.Path == "/api/v1/users/{id:[0-9]+}/profile/:tab" {
			foundClosureView = true
			if r.View != "users/profile.tmpl" || !reflect.DeepEqual(r.Params, []string{"id", "tab"}) {
				t.Errorf("unexpected closure route: %+v", r)
			}
			if !reflect.DeepEqual(r.Middlewares, []string{"Auth", "CORS", "throttle"}) {
				t.Errorf("unexpected merged middlewares: %v", r.Middlewares)
			}
		}
		if r.Path == "/api/v1/fallback" && r.View == "a.tmpl" {
			foundFallbackView = true
		}
	}
	if httpCount != 8 || grpcCount != 3 {
		t.Errorf("expected 8 HTTP and 3 gRPC routes, got http=%d grpc=%d", httpCount, grpcCount)
	}
	if !foundResourceShow || !foundClosureView || !foundFallbackView || !foundGrpcStream || !foundOrphanGrpc {
		t.Errorf("missing expected routes: show=%v closure=%v fallback=%v grpcStream=%v orphanGrpc=%v",
			foundResourceShow, foundClosureView, foundFallbackView, foundGrpcStream, foundOrphanGrpc)
	}
	if pm, ok := cat.ProtoMethods["UserService"]["StreamUsers"]; !ok || pm.ReqType != "Empty" || pm.ResType != "UserReply" {
		t.Errorf("unexpected proto method: %+v ok=%v", pm, ok)
	}
}

func TestIndexer_FrameworkExtrasViewsAndBootstrap(t *testing.T) {
	root := makeProject(t, map[string]string{
		"app/models/user.go": `package models
type User struct {
	ID    uint   ` + "`gorm:\"column:id\"`" + `
	Email string ` + "`gorm:\"column:email\"`" + `
}
func (User) TableName() string { return "users" }
func ActiveUsers(q orm.Query) orm.Query { return q }
`,
		"app/console/commands/sync_cmd.go": `package commands
type SyncUsers struct{}
func (c *SyncUsers) Signature() string { return "users:sync" }
func (c *SyncUsers) Description() string { return "Sync all users" }
`,
		"app/http/middleware/auth.go": `package middleware
func JwtAuth() {}
type RateLimiter struct{}
`,
		"database/seeders/user_seeder.go": `package seeders
type UserSeeder struct{}
func (s *UserSeeder) Signature() string { return "UserSeeder" }
`,
		"app/console/kernel.go": `package console
func Schedule() {
	facades.Schedule().Command("users:sync").Cron("*/5 * * * *")
	facades.Schedule().Call(func() {}).DailyAt("02:30")
	facades.Schedule().Command("cleanup").EveryFifteenMinutes()
}
`,
		"lang/en.json":          `{"welcome": "Hello", "nested": {"bye": "Goodbye"}}`,
		"lang/id/messages.json": `{"title": "Judul"}`,
		"lang/broken.json":      `{invalid`,
		"app/providers/auth_service_provider.go": `package providers
func Boot() {
	facades.Gate().Define("edit-post", func() {})
	facades.View().LoadViewsFrom(path.Base("vendor_views"), "pkg")
	facades.View().Share("app_name", "Goravel")
	_ = template.FuncMap{
		"upper": func(s string) string { return s },
		"join":  func(sep string, items ...string) string { return "" },
	}
}
`,
		"app/events/user_created.go": `package events
type UserCreated struct{}
`,
		"app/listeners/send_welcome.go": `package listeners
type SendWelcome struct{}
`,
		"app/providers/event_service_provider.go": `package providers
func Listen() map[any][]any {
	return map[any][]any{
		&events.UserCreated{}: {&listeners.SendWelcome{}},
	}
}
`,
		"app/jobs/email_job.go": `package jobs
type SendEmailJob struct{}
func (j *SendEmailJob) Signature() string { return "send_email" }
`,
		"app/observers/user_observer.go": `package observers
type UserObserver struct{}
func (o *UserObserver) Creating() error { return nil }
func (o *UserObserver) Deleted() error { return nil }
`,
		"app/rules/slug.go": `package rules
type SlugRule struct{}
func (r *SlugRule) Signature() string { return "slug" }
func (r *SlugRule) Message() string { return "must be slug" }
`,
		// Views: app overrides package view + module view + partial hierarchy with sub-field rebinding
		"vendor_views/pages/home.tmpl":                `{{ define "pages/home.tmpl" }}PKG{{ end }}`,
		"packages/admin/views/dashboard.tmpl":         `{{ define "dashboard.tmpl" }}ADMIN{{ end }}`,
		"modules/shop/resources/views/cart.tmpl":      `{{ define "shop/cart.tmpl" }}CART{{ end }}`,
		"resources/views/pages/home.tmpl":             `{{ define "pages/home.tmpl" }}{{ template "partials/header.tmpl" . }}{{ template "partials/user_card.tmpl" .user }}<h1>{{ .title }}</h1>{{ end }}`,
		"resources/views/partials/header.tmpl":        `{{ define "partials/header.tmpl" }}<nav>{{ .nav_title }}</nav>{{ end }}`,
		"resources/views/partials/user_card.tmpl":     `{{ define "partials/user_card.tmpl" }}<span>{{ .Email }}</span>{{ end }}`,
		"app/http/controllers/home_controller.go": `package controllers
type HomeController struct{}
func (c *HomeController) Index(ctx http.Context) {
	var user models.User
	data := map[string]any{"title": "Home", "user": user}
	data["nav_title"] = "Main"
	_ = path.View("pages", "home.tmpl")
	_ = facades.View().Exists("pages/home.tmpl")
	_ = ctx.Response().View().Make("pages/home.tmpl", data)
}
`,
		// Bootstrap directory with partial registrations to detect unregistered items
		"bootstrap/app.go": `package bootstrap
// intentionally empty registrations
`,
	})

	cat := Build(root)

	if len(cat.Commands) != 1 || cat.Commands[0].Signature != "users:sync" || cat.Commands[0].Description != "Sync all users" {
		t.Errorf("unexpected Commands: %+v", cat.Commands)
	}
	if _, ok := cat.Middleware["JwtAuth"]; !ok {
		t.Error("expected JwtAuth middleware")
	}
	if _, ok := cat.Middleware["RateLimiter"]; !ok {
		t.Error("expected RateLimiter middleware")
	}
	if len(cat.Seeders) != 1 || cat.Seeders[0].Signature != "UserSeeder" {
		t.Errorf("unexpected Seeders: %+v", cat.Seeders)
	}
	if len(cat.Schedules) != 3 {
		t.Fatalf("expected 3 schedules, got %d: %+v", len(cat.Schedules), cat.Schedules)
	}
	if !reflect.DeepEqual(cat.TranslationKeys(), []string{"messages.title", "nested.bye", "welcome"}) {
		t.Errorf("unexpected TranslationKeys: %v", cat.TranslationKeys())
	}
	if !reflect.DeepEqual(cat.GateAbilities(), []string{"edit-post"}) {
		t.Errorf("unexpected GateAbilities: %v", cat.GateAbilities())
	}
	if len(cat.Events) != 1 || !reflect.DeepEqual(cat.Events[0].Listeners, []string{"SendWelcome"}) {
		t.Errorf("unexpected Events: %+v", cat.Events)
	}
	if len(cat.Jobs) != 1 || cat.Jobs[0].Signature != "send_email" {
		t.Errorf("unexpected Jobs: %+v", cat.Jobs)
	}
	if len(cat.Observers) != 1 || cat.Observers[0].Model != "User" || len(cat.Observers[0].Hooks) != 2 {
		t.Errorf("unexpected Observers: %+v", cat.Observers)
	}
	if len(cat.Scopes) != 1 || cat.Scopes[0].Name != "ActiveUsers" || cat.Scopes[0].Model != "User" {
		t.Errorf("unexpected Scopes: %+v", cat.Scopes)
	}

	// Custom FuncMap
	if fn := cat.TemplateFuncs["upper"]; fn == nil || fn.MinArgs != 1 || fn.Variadic {
		t.Errorf("unexpected upper FuncMap: %+v", fn)
	}
	if fn := cat.TemplateFuncs["join"]; fn == nil || fn.MinArgs != 1 || !fn.Variadic {
		t.Errorf("unexpected join FuncMap: %+v", fn)
	}

	// Views & hierarchy propagation
	home := cat.LookupView("pages/home.tmpl")
	if home == nil || home.Source != "app" || len(home.PackageLocs) != 1 {
		t.Fatalf("expected app view overriding 1 package view, got %+v", home)
	}
	if cat.LookupView("dashboard.tmpl") == nil || cat.LookupView("shop/cart.tmpl") == nil {
		t.Errorf("expected packages/*/views and modules/*/resources/views indexed, got %v", cat.Views)
	}
	if cat.ViewForPath(home.PackageLocs[0].File) != home {
		t.Error("ViewForPath should match PackageLocs too")
	}
	// nav_title used in partials/header.tmpl should propagate up to pages/home.tmpl UsedVars
	usedMap := map[string]bool{}
	for _, u := range home.UsedVars {
		usedMap[u] = true
	}
	if !usedMap["title"] || !usedMap["nav_title"] {
		t.Errorf("expected propagated UsedVars on home, got %v", home.UsedVars)
	}
	// partials/header.tmpl should inherit title & nav_title from pages/home.tmpl
	headerVars := map[string]bool{}
	for _, vv := range cat.ViewVars["partials/header.tmpl"] {
		headerVars[vv.Key] = true
	}
	if !headerVars["title"] || !headerVars["nav_title"] {
		t.Errorf("expected propagated vars on partials/header.tmpl, got %+v", cat.ViewVars["partials/header.tmpl"])
	}
	// partials/user_card.tmpl should receive rebound User struct fields (.Email, .ID)
	cardVars := map[string]bool{}
	for _, vv := range cat.ViewVars["partials/user_card.tmpl"] {
		cardVars[vv.Key] = true
	}
	if !cardVars["Email"] || !cardVars["ID"] {
		t.Errorf("expected sub-field rebound struct fields on user_card.tmpl, got %+v", cat.ViewVars["partials/user_card.tmpl"])
	}

	// Unregistered bootstrap detection (SyncUsers, UserSeeder, SlugRule, SendEmailJob, UserObserver)
	unregKinds := map[string]bool{}
	for _, u := range cat.Unregistered {
		unregKinds[u.Kind] = true
	}
	for _, want := range []string{"command", "seeder", "rule", "job", "observer"} {
		if !unregKinds[want] {
			t.Errorf("expected unregistered %q item, got %+v", want, cat.Unregistered)
		}
	}
}

func TestFirstExportedStructAndPkg_RegressionNoPanicOnShortTypeLines(t *testing.T) {
	// Regression test for Bug #1: lines starting with "type " with fewer than 3 fields must not panic.
	cases := []struct {
		src        string
		wantStruct string
		wantPkg    string
	}{
		{"package mig\ntype (\n\tFoo struct{}\n)\n", "", "mig"},
		{"package mig\ntype Alias\n", "", "mig"},
		{"package mig\ntype M1 struct{}\n", "M1", "mig"},
		{"package mig\ntype M2 struct {\n}\n", "M2", "mig"},
	}
	for _, tc := range cases {
		st, pkg := firstExportedStructAndPkg(tc.src)
		if st != tc.wantStruct || pkg != tc.wantPkg {
			t.Errorf("firstExportedStructAndPkg(%q) = (%q, %q), want (%q, %q)", tc.src, st, pkg, tc.wantStruct, tc.wantPkg)
		}
	}
}

func TestLiveDB_HelpersAndMockedCLIs(t *testing.T) {
	// 1. ResolveDBConfig from catalog env and config fallback
	root := makeProject(t, map[string]string{
		".env": "DB_CONNECTION=sqlite\nDB_DATABASE=database/app.sqlite\n",
		"config/database.go": `package config
func init() {
	c := facades.Config()
	c.Add("database", map[string]any{
		"default": "postgres",
		"connections": map[string]any{
			"postgres": map[string]any{
				"host": "127.0.0.1",
				"port": "5432",
				"database": "mydb",
				"username": "pguser",
				"password": "secret",
			},
		},
	})
}
`,
	})
	// Ensure real env vars don't shadow test .env
	for _, k := range []string{"DB_CONNECTION", "DB_HOST", "DB_PORT", "DB_DATABASE", "DB_USERNAME", "DB_PASSWORD"} {
		t.Setenv(k, "")
	}

	cat := Build(root)
	cfg := ResolveDBConfig(cat)
	if cfg.Driver != "sqlite" || cfg.Database != filepath.Join(root, "database", "app.sqlite") {
		t.Errorf("unexpected sqlite DB config: %+v", cfg)
	}
	if err := cfg.validate(); err != nil {
		t.Errorf("valid sqlite config should pass validate(): %v", err)
	}

	// Fallback to config/database.go when .env omits DB_*
	delete(cat.EnvVars, "DB_CONNECTION")
	delete(cat.EnvVars, "DB_DATABASE")
	pgCfg := ResolveDBConfig(cat)
	if pgCfg.Driver != "postgres" || pgCfg.Host != "127.0.0.1" || pgCfg.Database != "mydb" || pgCfg.Username != "pguser" {
		t.Errorf("unexpected postgres fallback config: %+v", pgCfg)
	}
	if err := (dbConnConfig{Driver: "postgres"}).validate(); err == nil {
		t.Error("empty postgres config must fail validate()")
	}
	if err := (dbConnConfig{Driver: "sqlite"}).validate(); err == nil {
		t.Error("empty sqlite config must fail validate()")
	}

	// 2. SanitizeSQLPlaceholders
	gotSan := SanitizeSQLPlaceholders("SELECT * FROM u WHERE id = $1 AND code = '?' AND tag = ? AND p = @name")
	wantSan := "SELECT * FROM u WHERE id = '1' AND code = '?' AND tag = '1' AND p = '1'"
	if gotSan != wantSan {
		t.Errorf("SanitizeSQLPlaceholders=%q want %q", gotSan, wantSan)
	}

	// 3. tsvToJSON & mysqlArgs
	jsonOut := tsvToJSON("id\tname\n---\t---\n1\tAlice\n2\tNULL\n", "\t")
	if !strings.Contains(jsonOut, `"Alice"`) || !strings.Contains(jsonOut, `"name":null`) {
		t.Errorf("unexpected tsvToJSON output: %s", jsonOut)
	}
	if tsvToJSON("only_header", "\t") != "[]" {
		t.Error("header-only TSV must return []")
	}
	mArgs := mysqlArgs(dbConnConfig{Host: "h", Username: "u", Password: "pw", Database: "db"}, "3306", "-e", "SELECT 1")
	if strings.Join(mArgs, " ") != "-h h -P 3306 -u u -ppw -D db -e SELECT 1" {
		t.Errorf("unexpected mysqlArgs: %v", mArgs)
	}

	// 4. Delimited parsers + ForSQL filtering
	c2 := NewCatalog()
	tModel := c2.ensure("users")
	tModel.Model = "User"
	tModel.add(&Column{Name: "id", GoName: "ID", FromModel: true})
	tModel.add(&Column{Name: "model_only", GoName: "ModelOnly", FromModel: true})
	tModel.FromModel = true
	c2.Models["User"] = tModel
	c2.Migrations = []*MigrationItem{
		{Name: "20250101_create_users", Signature: "create_users"},
		{Name: "20250102_create_posts", Signature: "create_posts"},
	}

	parseDelimitedColumns(c2, "users|id|bigint|NO\nusers|live_only|text|YES\naudit_log|event|text|NO\nbad_line\n", "|")
	parseDelimitedRoutines(c2, "public|calc_total|FUNCTION|integer\n", "|")
	parseDelimitedMigrations(c2, "create_users|2\n---\n", "|")

	if !c2.LiveDBConnected {
		t.Fatal("expected LiveDBConnected=true")
	}
	if len(c2.DBFunctions) != 1 || c2.DBFunctions[0].Name != "calc_total" {
		t.Errorf("unexpected DBFunctions: %+v", c2.DBFunctions)
	}
	if c2.Migrations[0].Status != "applied" || c2.Migrations[0].Batch != 2 || c2.Migrations[1].Status != "pending" {
		t.Errorf("unexpected migration statuses: %+v %+v", c2.Migrations[0], c2.Migrations[1])
	}

	modelOnlyCat := c2.ForSQL(SQLSourceModel)
	if modelOnlyCat.Table("audit_log") != nil || modelOnlyCat.Table("users").Column("live_only") != nil || modelOnlyCat.Table("users").Column("model_only") == nil {
		t.Errorf("SQLSourceModel filtering failed: tables=%v", modelOnlyCat.TableNames())
	}
	liveOnlyCat := c2.ForSQL(SQLSourceLiveDB)
	if liveOnlyCat.Table("audit_log") == nil || liveOnlyCat.Table("users").Column("live_only") == nil || liveOnlyCat.Table("users").Column("model_only") != nil {
		t.Errorf("SQLSourceLiveDB filtering failed: tables=%v", liveOnlyCat.TableNames())
	}
	bothCat := c2.ForSQL(SQLSourceBoth)
	if bothCat.Table("audit_log") == nil || bothCat.Table("users").Column("model_only") == nil || bothCat.Table("users").Column("live_only") == nil {
		t.Errorf("SQLSourceBoth should keep merged tables/columns")
	}

	// 5. Mocked DB CLIs via temporary PATH (Unix)
	if runtime.GOOS != "windows" {
		fakeBin := t.TempDir()
		writeMockCLI := func(name, script string) {
			p := filepath.Join(fakeBin, name)
			if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		writeMockCLI("psql", `
case "$*" in
  *information_schema.columns*) printf "live_tbl|id|uuid|NO\n" ;;
  *information_schema.routines*) printf "public|fn_x|FUNCTION|text\n" ;;
  *FROM\ migrations*) printf "m1|1\n" ;;
  *EXPLAIN*) printf "Seq Scan on live_tbl\n" ;;
  *json_agg*) printf '[{"id":"1"}]\n' ;;
esac
`)
		writeMockCLI("sqlite3", `
case "$*" in
  *sqlite_master*) printf "sq_tbl|id|INTEGER|NO\n" ;;
  *FROM\ migrations*) printf "m1|3\n" ;;
  *EXPLAIN*) printf "SCAN sq_tbl\n" ;;
  *-json*) printf '[{"id":1}]\n' ;;
esac
`)
		t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

		// Postgres introspection + explain + preview
		pgCat := NewCatalog()
		pgCat.EnvVars["DB_CONNECTION"] = "postgres"
		pgCat.EnvVars["DB_HOST"] = "localhost"
		pgCat.EnvVars["DB_DATABASE"] = "testdb"
		pgCat.EnvVars["DB_USERNAME"] = "tester"
		IntrospectLiveDB(pgCat)
		if pgCat.Table("live_tbl") == nil || len(pgCat.DBFunctions) != 1 {
			t.Errorf("mocked psql introspection failed: tables=%v funcs=%v", pgCat.TableNames(), pgCat.DBFunctions)
		}
		if out, err := ExplainQuery(pgCat, "SELECT * FROM live_tbl WHERE id = $1"); err != nil || !strings.Contains(out, "Seq Scan") {
			t.Errorf("ExplainQuery(postgres) = (%q, %v)", out, err)
		}
		if out, err := PreviewQuery(pgCat, "SELECT * FROM live_tbl;"); err != nil || out != `[{"id":"1"}]` {
			t.Errorf("PreviewQuery(postgres) = (%q, %v)", out, err)
		}
		if _, err := PreviewQuery(pgCat, "DELETE FROM live_tbl"); err == nil {
			t.Error("PreviewQuery must reject non-SELECT/WITH statements")
		}

		// SQLite introspection + explain + preview
		sqCat := NewCatalog()
		sqCat.EnvVars["DB_CONNECTION"] = "sqlite"
		sqCat.EnvVars["DB_DATABASE"] = "/tmp/fake.db"
		IntrospectLiveDB(sqCat)
		if sqCat.Table("sq_tbl") == nil {
			t.Errorf("mocked sqlite3 introspection failed: %v", sqCat.TableNames())
		}
		if out, err := ExplainQuery(sqCat, "SELECT * FROM sq_tbl"); err != nil || !strings.Contains(out, "SCAN sq_tbl") {
			t.Errorf("ExplainQuery(sqlite) = (%q, %v)", out, err)
		}
		if out, err := PreviewQuery(sqCat, "WITH x AS (SELECT 1) SELECT * FROM x"); err != nil || out != `[{"id":1}]` {
			t.Errorf("PreviewQuery(sqlite) = (%q, %v)", out, err)
		}
	}
}

func TestCatalog_ConcurrentReadsRaceSafe(t *testing.T) {
	cat := Build(fixture)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				_ = cat.Table("ref_geo_cities")
				_ = cat.Model("RefGeoCity")
				_ = cat.Config("jwt.secret")
				_ = cat.ConfigKeys()
				_ = cat.Rule("required")
				_ = cat.RuleNames()
				_ = cat.TableNames()
				_ = cat.RoutesForHandler("CityController", "Index")
				_ = cat.TranslationKeys()
				_ = cat.GateAbilities()
				_ = cat.LookupView("missing.tmpl")
				_ = cat.ViewForPath("/missing.tmpl")
				_ = cat.ForSQL(SQLSourceModel)
			}
		}()
	}
	wg.Wait()
}
