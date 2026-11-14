# Goravel VS Code Extension — Feature Analysis & Implementation Roadmap

This document tracks the analysis of existing capabilities and implemented enhancements (**P0–P6**) for `goravel-vscode-extension`.

---

## Status Legend
- `[ ]` Planned (Backlog)
- `[~]` In Progress
- `[x]` Implemented & Verified

---

## 1. Capability Matrix (Baseline vs. Enhanced)

| Domain | Baseline (`v0.5.0`) | Enhanced (`Current`) |
|---|---|---|
| **Schema & Migrations** | `Schema().Create` & raw `CREATE TABLE` indexing; Model ← Migration drift Quick Fix | + Chronological `Schema().Table` (`DropColumn`, `RenameColumn`, add column) & raw `ALTER TABLE` parsing; bidirectional **Model ↔ Migration** cross-file Quick Fixes; live DB **Applied (batch N) / Pending** migration badges & inline Migrate/Rollback actions |
| **Bootstrap Wiring** | Read-only warnings for unregistered items | + One-click **Cross-File Quick Fix** & Sidebar button (`goravel.registerBootstrap`) to auto-register migrations (including `bootstrap/bootstrap_<module>/migrations.go`), seeders, rules, and commands |
| **ORM Query Builder & Models** | `.Where`, `.Select`, `.Order`, `.Group`, `.Update("col", v)`, `.Pluck`, `.With("Rel")` | + `.Join` / `.LeftJoin` / `.InnerJoin` / `.RightJoin` (table, column & FK `ON` completion + alias resolution), map-based `.Where(map[string]any{...})` & `.Updates(map[string]any{...})`, `.Distinct`, `.WhereColumn`, `.Increment`, `.Decrement`, nested `.With("Rel", func(q orm.Query) ...)` target-table inference, and **Model Observer & Scope CodeLens** |
| **Routes, gRPC & Controllers** | Route indexing, `ctx.Request().Route("param")`, controller CodeLens, `@Router` drift check | + `routes/*.go` **Inlay Hints** & **Hover** (`→ METHOD /full/path [Middlewares]`), **gRPC controllers & `.proto` service indexing** (`routes/grpc.go`, `app/grpc/controllers`), `ctx.Request().Input*("...")` & `Query*("...")` completion/hover/definition, and full **Swagger / OpenAPI annotation scaffolding** & `@Router` sync Quick Fixes |
| **Views & Templates** | `View().Make("...")` template path completion & unused key hint | + Full `facades.View()` (`Exists`, `LoadViewsFrom`, `Share`, `Shared`), `View().First(...)`, & `path.View(...)` support; package view override resolution; **`.tmpl` / `.gohtml` syntax highlighting grammar**; `.tmpl` LSP (`{{ template }}` navigation, context/shared/inherited vars, nested model struct fields, local `$vars`, diagnostics, Quick Fix scaffolding, cross-file rename/refs, and template CodeLens) |
| **Config & `.env`** | `facades.Config().Get*` completion, hover, inlay hints, go-to-definition | + Missing `.env` key diagnostics on `config.Env("KEY")` and cross-file Quick Fix to append missing keys to `.env.example` / `.env` |
| **Localization (`Lang`)** | *Not supported* | + Full `lang/**/*.json` indexing (flat & nested keys), `facades.Lang(ctx).Get("...")` completion, multi-locale Markdown hover, inline preview inlay hints, go-to-definition, references, rename, missing-key diagnostics, and Quick Fix to add keys to `lang/en.json` |
| **Authorization (`Gate`)** | *Not supported* | + Indexes `facades.Gate().Define("ability", ...)` across `app/` & `bootstrap/`; provides completion, hover, definition, references, rename, and unknown-ability diagnostics on `Allows`, `Denies`, `Inspect`, `Check`, `Any`, `None` |
| **Events, Listeners & Jobs** | *Not supported* | + Indexes `app/events`, `app/listeners`, `app/jobs` & provider event maps; Event struct **CodeLens** (`N listener(s)`), sidebar sections, and unregistered Job detection |
| **Raw SQL & Live DB** | SQL completion, hover, diagnostics, `SELECT *` expand, N+1 warning, Explain/Preview on static SQL | + Automatic placeholder sanitization (`$1`, `?`, `@p1` → `NULL`) for parameterized **Explain** & **Preview**; Quick Fix to insert missing SQL scan columns into Go destination structs |
| **Refactoring (`Rename`)** | *Not supported* | + Workspace-wide `textDocument/rename` for Config keys, Custom Validation Rules, Gate abilities, Translation keys, SQL aliases, and Table columns |
| **Sidebar & Webviews** | 4 TreeViews + basic Artisan runner | + Inline action buttons on Tables, Routes & Unregistered items; **Database ER Diagram** Webview (`goravel.showErDiagram`); searchable **Route Matrix** Webview (`goravel.showRouteMatrix`); configurable `goravel.artisan.command` & 10 new `make:*` / `package:install` commands |

---

## 2. Implemented Roadmap (P0 → P3)

### P0 — Immediate High-Impact UX & Bootstrap Automation

- [x] **P0.1: Sidebar Inline Action Buttons & Richer Navigation**
  - Added inline buttons (`view/item/context`) on **Routes** tree items (`goravelRoute`): `Test Route Request` (`$(send)`) and `Open Route Registration` (`$(go-to-file)`).
  - Added inline buttons on **Models & Schema** tree items (`goravelTable`): `Preview Table Data (Top 20)` (`$(eye)`) and `Open Table Migration` (`$(file-code)`).
  - Added inline action on **Unregistered** tree items (`goravelUnregistered`): `Register in bootstrap/` (`$(add)`).
  - *Files:* `package.json`, `client/src/views/explorers.ts`, `client/src/extension.ts`.
- [x] **P0.2: Cross-File Code Action to Auto-Register Unregistered Components in `bootstrap/`**
  - When a migration, seeder, custom validation rule, or console command is not registered in `bootstrap/`, provides a Quick Fix (`Register <Item> in bootstrap/...`) that edits the corresponding `bootstrap/*.go` or `bootstrap/bootstrap_<module>/migrations.go` file via cross-file `WorkspaceEdit`.
  - Also supports triggering registration directly from the Sidebar `Unregistered in bootstrap/` node via `goravel/registerBootstrap`.
  - *Files:* `server/internal/framework/drift.go`, `server/internal/framework/types.go`, `server/internal/lsp/server.go`, `client/src/extension.ts`.
- [x] **P0.3: Configurable Artisan Runner & Complete `make:*` / `package:install` Presets**
  - Added `goravel.artisan.command` setting (default `"go run . artisan"`, customizable for env prefixes or Docker).
  - Expanded `ARTISAN_PRESETS`, commands, and `explorer/context` menus with `make:event`, `make:listener`, `make:job`, `make:observer`, `make:policy`, `make:provider`, `make:factory`, `make:test`, `make:filter`, and `package:install`.
  - *Files:* `package.json`, `client/src/commands/artisan.ts`.

---

### P1 — Deepening Core Schema, ORM, Route & Controller Intelligence

- [x] **P1.1: Incremental Migration Parsing (`Schema().Table` & `ALTER TABLE`) + Bidirectional Drift Quick Fixes**
  - Sorted migration files chronologically and parsed `facades.Schema().Table("...", func(table schema.Blueprint) { ... })` (`DropColumn`, `RenameColumn`, and column additions) plus raw SQL `ALTER TABLE ... ADD COLUMN / DROP COLUMN / RENAME COLUMN` in `server/internal/indexer/migrations.go`.
  - Added reverse Quick Fix (`buildModelToMigrationAction`) in `server/internal/framework/drift.go`: when a Go model field is missing from its migration, offers a cross-file Quick Fix to insert the column into the migration file (supports both `Schema().Create` blueprints and raw `CREATE TABLE` SQL).
  - *Files:* `server/internal/indexer/migrations.go`, `server/internal/indexer/schema.go`, `server/internal/framework/drift.go`.
- [x] **P1.2: Advanced ORM Query Builder Intelligence (`Join`, Map `.Where`/`.Updates`, `.With` Callback Scope)**
  - Provided table, column, and FK join condition completion (`LEFT JOIN <target> ON ...`), hover, definition, and diagnostics inside `.Join("...")` / `.InnerJoin("...")` / `.LeftJoin("...")` / `.RightJoin("...")` strings, plus alias registration for subsequent `.Where`/`.Select` calls.
  - Added support for map literal keys in `.Where(map[string]any{...})`, `.OrWhere(map[string]any{...})`, `.Update(map[string]any{...})`, and `.Updates(map[string]any{...})`.
  - Added support for `.Distinct("col")`, `.WhereColumn("col1", "col2")`, `.Increment("col")`, and `.Decrement("col")`.
  - Inferred the target relation table inside nested callbacks `.With("Relation", func(q orm.Query) orm.Query { ... })`.
  - *Files:* `server/internal/orm/analyzer.go`.
- [x] **P1.3: Controller Request Input/Query Completion & Route Inlay Hints**
  - In `routes/*.go`, added **Inlay Hints** (`RouteInlayHints`) and **Hover** (`RouteAndControllerHover`) on route definitions showing the full resolved URL path and inherited middlewares (e.g. `→ GET /api/geo/cities [Jwt]`).
  - In controllers (`app/http/controllers/*.go`), added completion, hover, and definition (`SpanRequestInput`) for `ctx.Request().Input("...")`, `ctx.Request().Query("...")`, `ctx.Request().InputInt("...")`, etc. based on bound `requests.*` struct fields and route parameters.
  - Added Quick Fix in `ControllerCodeActions` to update out-of-sync Swagger `@Router` annotations to match the registered route.
  - *Files:* `server/internal/framework/routes_check.go`, `server/internal/orm/analyzer.go`, `server/internal/lsp/server.go`.

---

### P2 — Framework Ecosystem Expansion (i18n, Gates, `.env`, Live DB Enhancements)

- [x] **P2.1: Localization / Translation (`facades.Lang(ctx).Get(...)`) Intelligence**
  - Indexed JSON translation files in `lang/` (`lang/en.json`, `lang/<locale>/**/*.json`, both flat and nested keys).
  - Added Autocompletion, Hover (Markdown table across all locales), Inlay Hints (inline preview of default translation string), Go to Definition, Find References, missing-key Diagnostics, and Quick Fix (`Add translation key to lang/en.json`) for `facades.Lang(ctx).Get("...")`.
  - *Files:* `server/internal/indexer/extras.go`, `server/internal/orm/analyzer.go`, `server/internal/framework/routes_check.go`.
- [x] **P2.2: Authorization Gates (`facades.Gate()`) Intelligence**
  - Indexed `facades.Gate().Define("<ability>", ...)` declarations across `app/` and `bootstrap/`.
  - Added Autocompletion, Hover, Go to Definition, Find References, Rename, and Diagnostics on `facades.Gate().Allows("...")`, `Denies("...")`, `Inspect("...")`, `Check("...")`, `Any(...)`, `None(...)`.
  - *Files:* `server/internal/indexer/extras.go`, `server/internal/orm/analyzer.go`, `server/internal/lsp/references.go`, `server/internal/lsp/rename.go`.
- [x] **P2.3: `.env` ↔ `config/*.go` Sync Diagnostics & Quick Fixes**
  - Added diagnostics in `config/*.go` when `config.Env("KEY")` references a key not present in `.env*`.
  - Added Quick Fix (`Add KEY to .env.example`) for missing env keys in `EnvAndLangCodeActions`.
  - *Files:* `server/internal/orm/analyzer.go`, `server/internal/framework/routes_check.go`.
- [x] **P2.4: Parameterized SQL Explain / Preview & Missing Scan Struct Field Quick Fix**
  - Added `SanitizeSQLPlaceholders` in `server/internal/indexer/livedb.go` to replace `$1`, `?`, and `@p1` placeholders with `NULL` outside string literals so `goravel.explainQuery` and `goravel.previewQuery` work seamlessly on parameterized queries.
  - Added Quick Fix (`AddMissingScanFields`) in `server/internal/rawsql/actions.go` to insert missing scan columns directly into the destination Go struct.
  - *Files:* `server/internal/indexer/livedb.go`, `server/internal/rawsql/actions.go`, `server/internal/lsp/server.go`.

---

### P3 — Refactoring, Architecture Visualization & Event/Job Tooling

- [x] **P3.1: Rename Symbol (`textDocument/rename`) Support**
  - Implemented `textDocument/rename` across the workspace for:
    - Config keys (`config/*.go` and `facades.Config().Get*("...")`)
    - Custom Validation Rule signatures (`app/rules/*.go` and `Rules(ctx)` strings)
    - Gate ability names (`facades.Gate().Define` and `Allows`/`Denies`/etc.)
    - Translation keys (`facades.Lang(ctx).Get` and `lang/*.json`)
    - SQL table/column aliases within a query and table column references across Go models, migrations, ORM calls, and raw SQL queries.
  - *Files:* `server/internal/lsp/rename.go`, `server/internal/lsp/server.go`.
- [x] **P3.2: Interactive Database ER Diagram & Route Matrix Webviews**
  - Added `Goravel: Show Database ER Diagram` (`goravel.showErDiagram`) command and Models view title button rendering an interactive schema overview with click-to-open Model/Migration buttons and generated Mermaid `erDiagram` source (`goravel/erDiagram`).
  - Added `Goravel: Show Route Matrix` (`goravel.showRouteMatrix`) command and Routes view title button rendering a searchable, method-filterable route table with one-click HTTP test request generation and Controller/Route navigation.
  - *Files:* `server/internal/lsp/server.go`, `client/src/extension.ts`, `package.json`.
- [x] **P3.3: Events, Listeners & Jobs Indexing + CodeLens**
  - Indexed `app/events/`, `app/listeners/`, and `app/jobs/`, plus event-to-listener registrations (`map[event.Event][]event.Listener`).
  - Added CodeLens on Event structs jumping to linked Listeners, sidebar sections for **Events & Listeners** and **Queue Jobs**, and drift detection for unregistered Jobs.
  - *Files:* `server/internal/indexer/extras.go`, `server/internal/lsp/server.go`, `client/src/views/explorers.ts`.

---

### P4 — Live Migration Status, Observers/Scopes, Template Intelligence, gRPC & OpenAPI Scaffolding

- [x] **P4.1: Live Migration Applied/Pending Status in Sidebar**
  - Extracted migration `Signature()` and queried the live database `migrations` table (PostgreSQL, MySQL, SQLite, SQL Server) when `goravel.sql.liveIntrospection` is enabled (`ApplyLiveMigrationStatus`).
  - Badged each migration in the **Migrations** TreeView as `Applied (batch N)` vs. `Pending`, with inline `Migrate` (`$(play)`) and `Rollback` (`$(discard)`) buttons.
  - *Files:* `server/internal/indexer/schema.go`, `server/internal/indexer/migrations.go`, `server/internal/indexer/livedb.go`, `client/src/views/explorers.ts`, `package.json`.
- [x] **P4.2: Model Observers & Scopes Intelligence**
  - Indexed `app/observers/` structs, lifecycle hook methods (`Creating`, `Created`, `Updating`, `Updated`, `Saving`, `Saved`, `Deleting`, `Deleted`, `Restoring`, `Restored`, `ForceDeleting`, `ForceDeleted`, `Retrieved`), and `facades.Orm().Observe(models.User{}, &observers.UserObserver{})` registrations.
  - Indexed reusable ORM query scopes (`func Scope...(q orm.Query) orm.Query`) in `app/models/`.
  - Added **CodeLens** on Model structs jumping to attached Observers and Query Scopes, plus a **Model Observers** section in the Sidebar and unregistered Observer detection.
  - *Files:* `server/internal/indexer/schema.go`, `server/internal/indexer/extras.go`, `server/internal/lsp/server.go`, `client/src/views/explorers.ts`.
- [x] **P4.3: Template Variable Completion in `resources/views/`**
  - Indexed controller and route `View().Make("view.tmpl", map[string]any{...})` / `View().First(...)` context map keys, value expressions, and locations (`Catalog.ViewVars`).
  - Enabled LSP `completion`, `hover`, and `definition` inside `.tmpl`, `.html`, and `.gohtml` template files under `resources/views/` when editing inside `{{ ... }}` blocks.
  - *Files:* `server/internal/indexer/extras.go`, `server/internal/framework/views.go`, `server/internal/lsp/server.go`, `client/src/extension.ts`.
- [x] **P4.4: gRPC Controllers & Proto Linkage**
  - Indexed `.proto` services/RPCs (`proto/`, `app/grpc/`, `contracts/proto/`), gRPC controllers (`app/grpc/controllers`), and `Register<Service>Server(...)` calls in `routes/grpc.go` and providers.
  - Surfaced `gRPC` routes (`/Service/Method`) alongside HTTP routes in the **Routes Explorer**, **Route Matrix** (with `gRPC` method filter), and Controller CodeLens.
  - *Files:* `server/internal/indexer/routes.go`, `server/internal/indexer/schema.go`, `client/src/views/explorers.ts`, `client/src/extension.ts`.
- [x] **P4.5: Auto-Generate Swagger / OpenAPI Annotations for Controllers**
  - Upgraded `ControllerCodeActions` to scaffold complete `@Summary`, `@Tags`, `@Accept json`, `@Produce json`, `@Security BearerAuth` (when auth middleware is attached), path `@Param`, query `@Param` (from `ctx.Request().Query*("...")` and GET `requests.*` structs), body `@Param` (from bound `requests.*` structs), inferred `@Success` response model (`{object} models.X` / `{array} models.X`), and normalized `@Router` annotations.
  - *Files:* `server/internal/framework/routes_check.go`.

---

### P5 — `facades.View` Intelligence & `.tmpl` Syntax Highlighting + Full LSP

- [x] **P5.1: `.tmpl` / `.gohtml` Syntax Highlighting, Language Configuration & Snippets**
  - Registered `goravel-tmpl` language (`language-configuration.tmpl.json`), `text.html.goravel-tmpl` TextMate grammar (`syntaxes/goravel-tmpl.tmLanguage.json`), and Go template snippets (`snippets/goravel-tmpl.code-snippets`).
  - *Files:* `package.json`, `language-configuration.tmpl.json`, `syntaxes/goravel-tmpl.tmLanguage.json`, `snippets/goravel-tmpl.code-snippets`, `client/src/extension.ts`.
- [x] **P5.2: Package Views (`LoadViewsFrom`), Shared View Data (`Share`/`Shared`), `First`, `Exists`, & `path.View` Indexing**
  - Indexed `facades.View().LoadViewsFrom(...)` directories (`packages/**/views`), tracking app view vs. package view override precedence (`ViewItem`).
  - Indexed `facades.View().Share("key", val)` (`SharedViewVars`), `View().First([]string{...}, data)`, `View().Exists("...")`, and `path.View("...")`.
  - Inferred Go struct/model types (`GoType`) for template variables and propagated parent `ViewVars` to partials included via `{{ template "..." . }}`.
  - *Files:* `server/internal/indexer/schema.go`, `server/internal/indexer/extras.go`.
- [x] **P5.3: Enhanced `facades.View` & `ResponseView` Intelligence in Go Files**
  - Provided completion, rich hover (source, override status, `{{ define }}` name, included partials, variables), go-to-definition (active + fallback locations), transitive unused-key diagnostics, and Quick Fix to scaffold missing `.tmpl` files.
  - *Files:* `server/internal/framework/views.go`, `server/internal/lsp/server.go`.
- [x] **P5.4: Full `.tmpl` / `.gohtml` LSP (Completion, Hover, Definition, References, Rename, Diagnostics, Quick Fixes, CodeLens)**
  - Provided `.tmpl` completion for `{{ template "..." }}` / `{{ block "..." }}`, context/shared/inherited `.vars`, nested `.model.Field` struct fields, local `$vars`, and Go template keywords/built-ins.
  - Provided `.tmpl` hover, go-to-definition, cross-file Find References & Rename (`F2`), template diagnostics (missing partial, mismatched `define`, unclosed/unbalanced blocks), Quick Fixes, and top-of-file CodeLens (routes/controllers rendering the view, parent includes, and package overrides).
  - *Files:* `server/internal/framework/views.go`, `server/internal/lsp/server.go`, `server/internal/lsp/references.go`, `server/internal/lsp/rename.go`, `server/internal/lsp/features_test.go`.

---

### P6 — Deep `facades.View` & `.tmpl` Intelligence (`P6.1`–`P6.12`)

- [x] **P6.1: Custom `template.FuncMap` Indexing & LSP (`config/http.go` + Service Providers)**
  - Indexed custom `template.FuncMap{...}` functions (`Name`, `Signature`, `Params`, `ReturnType`, `MinArgs`, `Variadic`, `Loc`) in `Catalog.TemplateFuncs`.
  - Added pipe `|` priority completion, hover signature & file:line, go-to-definition (`config/http.go`), and unknown/under-applied function diagnostics in `.tmpl` files.
  - *Files:* `server/internal/indexer/schema.go`, `server/internal/indexer/extras.go`, `server/internal/framework/views.go`.
- [x] **P6.2: Route Closures & `RouteItem.View` Linkage**
  - Linked anonymous `facades.Route().Get("/path", func(ctx http.Context) http.Response { ... })` closures and controller handlers rendering views to `RouteItem.View` and `ViewCaller.Route`.
  - *Files:* `server/internal/indexer/routes.go`, `server/internal/indexer/extras.go`, `server/internal/indexer/schema.go`.
- [x] **P6.3: Go Data Map Key Completion, Missing Variable Diagnostics & "Fill Missing Template Variables" Code Action**
  - Added autocompletion for expected `.tmpl` variable keys inside `View().Make("...", map[string]any{ "|" })`, hints for missing variables, and a Quick Fix to auto-fill missing template variables into the Go map literal.
  - *Files:* `server/internal/framework/views.go`.
- [x] **P6.4: `.tmpl` Unpassed / Mismatched Variable Diagnostics & Quick Fixes**
  - Detected `.var` references in `.tmpl` not passed by callers/shared vars, with Levenshtein/case-insensitive fuzzy matching (e.g. `.supabaseUrl` → `.supabase_url`) and Quick Fixes to rename the variable or insert the key into the controller's `View().Make` map.
  - *Files:* `server/internal/framework/views.go`.
- [x] **P6.5: Deep `GoType` Inference & Multi-Level `.a.b.c` Chains**
  - Indexed structs across `app/http/responses`, `app/services`, `app/http/controllers`, and `pkg/` plus function/method return types (`Catalog.FuncReturns`), supporting arbitrary-depth `.profile.User.Email` completion, hover, and go-to-definition.
  - *Files:* `server/internal/indexer/models.go`, `server/internal/indexer/extras.go`, `server/internal/framework/views.go`.
- [x] **P6.6: Multi-Hop Template DAG, Multi-`define` & Scoped `with`/`range` Context**
  - Upgraded `propagateTemplateHierarchy` to a multi-hop fixed-point loop, indexed multiple `{{ define }}` blocks per file, supported sub-field context rebinding (`{{ template "sub" .user }}`), and resolved scoped `.Field` vs root `$.var` inside `{{ with }}` and `{{ range }}` blocks.
  - *Files:* `server/internal/indexer/extras.go`, `server/internal/framework/views.go`.
- [x] **P6.7: Empty `.tmpl` & Orphaned View Detection + Scaffolding Quick Fixes**
  - Flagged empty `.tmpl` files with a Quick Fix to scaffold `{{ define "<view>" }}`, and added an unreferenced/orphaned view CodeLens indicator for templates with 0 callers and 0 parent includes.
  - *Files:* `server/internal/framework/views.go`.
- [x] **P6.8: Local Map Mutation Tracking, Direct Struct Context & `GetShared()`/`RegisteredViews()` Hover**
  - Tracked `data["key"] = val` mutations on local map variables, supported passing a struct directly to `View().Make("view", structVal)`, and added hover previews on `facades.View().GetShared()` and `facades.View().RegisteredViews()`.
  - *Files:* `server/internal/indexer/extras.go`, `server/internal/framework/views.go`.
- [x] **P6.9: HTML Route URL (`href`, `action`, `fetch`) & `public/` Static Asset Intelligence in `.tmpl`**
  - Added route URL and `public/` static asset completion, hover, go-to-definition, and broken static asset diagnostics inside `.tmpl` HTML attributes and JS `fetch`/`axios` calls.
  - *Files:* `server/internal/framework/views.go`.
- [x] **P6.10: `.tmpl` `DocumentSymbol`, `InlayHint`, TextMate Injections & Alpine.js Support**
  - Added `textDocument/documentSymbol` and `textDocument/inlayHint` for `.tmpl` files, removed the conflicting `"meta.embedded.block.goravel-tmpl": "go"` grammar mapping, injected `#tmpl-action` into `<script>`, `<style>`, and HTML attributes, and added Alpine.js directive highlighting & snippets.
  - *Files:* `server/internal/framework/views.go`, `server/internal/lsp/server.go`, `package.json`, `syntaxes/goravel-tmpl.tmLanguage.json`, `snippets/goravel-tmpl.code-snippets`.
- [x] **P6.11: Sidebar "Views & Templates" TreeView & Route Matrix View Column**
  - Added `ViewsTreeProvider` (`goravel.views.templates`) showing App Views, Package Views, Shared Variables (`View().Share`), and Custom `FuncMap` Functions, and added a `View` column to the Route Matrix webview and Routes TreeView.
  - *Files:* `package.json`, `client/src/views/explorers.ts`, `client/src/extension.ts`, `server/internal/lsp/server.go`.
- [x] **P6.12: Folder-Aware `make:view` with Layout Picker**
  - Enhanced `goravel.artisan.makeView` to pre-fill the subfolder path from the Explorer context URI and offer a QuickPick for layout scaffolding (`Page with Header & Footer Partials`, `Standalone {{ define }} Block`, or `artisan make:view` CLI).
  - *Files:* `client/src/commands/artisan.ts`, `package.json`.

---

## 3. Verification & Test Suite

- **Go LSP Unit Tests:** `server/internal/lsp/features_test.go` (`npm run test:server`)
  - `TestAlterMigrationAndTranslationsAndGates` — Verifies chronological `Schema().Table` (`RenameColumn`, `DropColumn`, column additions), `lang/*.json` flat/nested indexing, `facades.Gate().Define` indexing, Event → Listener mapping, `ResolveBootstrapRegistrationAction`, and reverse Model → Migration Quick Fix.
  - `TestORMJoinMapWhereWithCallbackAndLangGate` — Verifies `.LeftJoin`, map-based `.Where(map[string]any{...})`, nested `.With("Posts", func(q orm.Query) ...)` callback table inference, missing translation key diagnostics & inlay hints, and unknown Gate ability diagnostics.
  - `TestSanitizeSQLPlaceholdersAndERDiagramAndRename` — Verifies `SanitizeSQLPlaceholders` (`$1`, `?`, `@p1`), `goravel/erDiagram` Mermaid output, and workspace-wide `textDocument/rename`.
  - `TestP4LiveMigrationsObserversTemplatesGrpcAndSwagger` — Verifies live migration `applied` (with batch number) vs `pending` status (`P4.1`), Observer & Scope indexing + Model CodeLens (`P4.2`), `.tmpl` template variable completion/hover/definition (`P4.3`), gRPC controller & `.proto` route indexing (`P4.4`), and full Swagger/OpenAPI annotation Code Action generation (`P4.5`).
  - `TestP5ViewFacadeAndTemplateLSP` — Verifies `facades.View().LoadViewsFrom(...)` & app view override precedence, `Exists`/`First`/`Share`/`Shared` intelligence, sub-template context propagation, `.tmpl` completion (partials, nested model fields, local `$vars`, shared vars, keywords), `.tmpl` diagnostics/Quick Fixes/CodeLens, and cross-file Rename between Go controllers and `.tmpl` files (`P5.1`–`P5.4`).
  - `TestP6ViewAndTemplateEnhancements` — Verifies custom `template.FuncMap` indexing/hover/definition (`P6.1`), route closure `RouteItem.View` linkage (`P6.2`), Go `View().Make` data map key completion & "Fill missing template variables" Quick Fix (`P6.3`), `.tmpl` unpassed variable fuzzy-match diagnostics (`.supabaseUrl` → `.supabase_url`) (`P6.4`), deep `GoType` inference via service return types & multi-level `.profile.User.Email` navigation (`P6.5`), multi-hop `UsedVars` propagation & scoped `{{ with .profile }}` `.Bio` resolution (`P6.6`), empty `.tmpl` Quick Fix & orphaned view CodeLens (`P6.7`), `data["mutated_key"]` tracking & `GetShared()` hover (`P6.8`), HTML `href="/signin"` route intelligence & `public/` missing static asset diagnostics (`P6.9`), and `TemplateDocumentSymbols` (`P6.10`).
- **Client Typecheck & Bundle:** `npm run typecheck && npm run build`
