# Changelog

All notable changes to the **Goravel** VS Code extension (`goravel-vscode`) and `goravel-lsp` are documented in this file, organized by git release tag.

## [v1.0.0] - 2026-11-22

First stable release of **Goravel** (`goravel-vscode`) and `goravel-lsp`, hardened with full-stack automated test coverage, static analysis, race detection, and CI release gating.

### Fixes
- Prevent `sync.RWMutex` lock copying in `Catalog.ForSQL` (`go vet copylocks`) and guard against panics on short `type` declarations during workspace indexing (`afb83a3`)
- Fix subquery alias detection (`isFalseAliasKeywordMatch`) and add bounds clamping in raw SQL completion and context helpers (`afb83a3`)
- Fix malformed markdown backticks in `facades.View` completion documentation (`afb83a3`)
- Fix `goravel.registerBootstrap` response handling when the server writes `bootstrap/*.go` directly and clear the language client reference on `deactivate()` (`afb83a3`)
- Clean up temporary download directories when `install.sh` falls back to building from source (`afb83a3`)
- Preserve explicit `--all` flag in `action.yml` instead of overriding it with the default `only-sql: "true"` input (`afb83a3`)

### Tests
- Expand Go backend test coverage across `server/main.go`, `indexer`, `rawsql`, `orm`, `framework`, and `lsp` (from 31.4% to 83.2% total statements) with race detection and Go fuzz tests for SQL and template parsers (`afb83a3`)
- Add TypeScript client test suite covering `extension.ts`, `commands/artisan.ts`, `views/explorers.ts` (98.6% line coverage), and `package.json`/grammar/snippet consistency (`afb83a3`)
- Add isolated offline tests for `install.sh` and `action.yml`, including end-to-end CLI validation (`afb83a3`)

### CI/CD
- Gate `.github/workflows/publish.yml` and pull requests with `go vet`, Go tests, Go `-race` tests, TypeScript type checking, client tests, script tests, production build, and VSIX packaging validation (`afb83a3`)
- Add unified `npm test`, coverage, race, vet, and packaging scripts in `package.json` and exclude test artifacts in `.vscodeignore` (`afb83a3`)

### Refactoring & Documentation
- Extract testable `runCLI` and error-returning `parseSQLSourceFlag` in `server/main.go` and support configurable `Stdout` in `lsp.CheckOptions` (`afb83a3`)
- Update `README.md` with local testing, coverage, fuzzing, and packaging commands (`afb83a3`)

### Chore
- Bump extension version to `1.0.0`

## [v0.9.2] - 2026-11-22

### Features
- Add `--check-templates` / `only-templates` support for validating Go HTML template variables in `goravel-lsp` CLI and GitHub Actions (`action.yml`), plus `modules/` view and route indexing (`281c802`)

### Fixes
- Escape backslashes in Windows shell quoting and update dependencies (`e191257`)

## [v0.9.1] - 2026-11-16

### Features
- Add custom brand icon (`media/goravel.svg`) for the Goravel activity bar view container (`e195bb7`)

### Tests
- Add comprehensive test suite for framework analyzers and workflows (`5747afc`)
- Remove redundant internal package test files (`a0531da`)

## [v0.9.0] - 2026-11-16

### Features
- Implement `prepareRename` LSP capability and optimize webview panel lifecycle and route matrix rendering (`84548a6`)

### Performance
- Add concurrent file checking and document analysis caching in `goravel-lsp` server (`cb2790d`)

### Refactoring
- Improve SQL parsing, drift analysis, and raw query diagnostics (`ab3cf31`)
- Improve security, robustness, and stability across client extension and language server (`82fdb0e`)

### Chore
- Bump extension version to `0.9.0` (`763b685`)

## [v0.8.6] - 2026-11-15

### Features
- Add configurable SQL schema source options (`auto`, `model`, `live-db`, `both`) for Go models/migrations and live database introspection (`120c469`)

### Chore
- Remove roadmap and analysis documentation files (`d40e52f`)

## [v0.8.5] - 2026-11-15

### Features
- Support multi-root workspaces in LSP features and bump extension version to `0.8.5` (`9b0f8c5`)
- Enhance raw SQL CTE column analysis and template variable error diagnostics (`3efaaff`)

## [v0.8.3] - 2026-11-15

### Chore
- Update extension description and bump version to `0.8.3` (`192ac05`)

## [v0.8.2] - 2026-11-15

### Documentation
- Add usage examples and features to `README.md` (`c43a6f4`)
- Add `CHANGELOG.md` and `README.md` for `goravel-vscode` extension (`705496a`)

## [v0.8.1] - 2026-11-15

### Tests & Chore
- Add unit tests for raw SQL extraction and bump extension version to `0.8.1` (`87ee4a9`)

## [v0.8.0] - 2026-11-15

### Features
- Add GitHub Action support (`action.yml`) and cross-platform `goravel-lsp` binary releases (`42e10f3`)

## [v0.7.2] - 2026-11-15

### Refactoring
- Enhance template hover features with span tracking and detailed type resolution (`74e5c46`)

## [v0.7.1] - 2026-11-15

### Features
- Add **Views & Templates** explorer view and enhance `make:view` command scaffolding (`5fc9454`)
- Analyze `facades.View` and `.tmpl` enhancement opportunities (`2a9f0c0`)

### Refactoring
- Add `omitempty` JSON tag to `created_at` and `updated_at` fields in models (`b6e0ff9`)

## [v0.7.0] - 2026-11-14

### Features
- Add full `facades.View` intelligence and `.tmpl` / `.gohtml` (`goravel-tmpl`) syntax highlighting with LSP support (`0716901`)

## [v0.6.1] - 2026-11-14

### Features
- Add support for model observers, gRPC routes, and live migration status (`5e28414`)

## [v0.6.0] - 2026-11-14

### Features
- Add new Artisan commands, LSP code navigation features, and workspace overview items (`b9503d8`)

### Refactoring
- Remove outdated implementation documentation and test files (`34faeae`)

## [v0.5.2] - 2026-11-14

### Features
- Add live database fallbacks, multi-dialect support, and SQL diagnostics (`9eb6509`)

### Chore
- Upgrade GitHub Actions versions in publish workflow (`4a5f024`)

## [v0.5.0] - 2026-11-13

### Refactoring
- Improve test runner, add configuration change handling, and document implementation gaps (`d8f14f6`)

## [v0.4.3] - 2026-11-13

### Features
- Support hover, go-to-definition, and semantic tokens for table name variables in raw SQL (`6367194`)

## [v0.4.2] - 2026-11-13

### Features
- Add support for SQL column aliases, CTEs, and subqueries in LSP (`2a4d868`)

## [v0.4.1] - 2026-11-13

### CI/CD
- Update `.vsix` artifact path to `dist/` directory in publish workflow (`abc41c9`)

## [v0.4.0] - 2026-11-13

### Features
- Enhance extension with Artisan seeding (`db:seed...`), table data preview, and code snippets (`592ce69`)

## [v0.3.2] - 2026-11-12

### Refactoring
- Update Go version and refine SQL injection syntax regex patterns (`8f8c3e0`)

## [v0.3.1] - 2026-11-12

### Fixes
- Restrict SQL injection grammar scope with string boundary checks (`33e2fff`)

## [v0.3.0] - 2026-11-12

### Features
- Add support for indexing custom Artisan commands and route testing requests (`6907ae6`)

## [v0.2.5] - 2026-11-12

### CI/CD
- Add GitHub Release step and update repository permissions for publishing (`f77955a`)

## [v0.2.4] - 2026-11-12

### Chore
- Re-tagged release on `7a5e5a9` (same commit as `v0.2.3`)

## [v0.2.3] - 2026-11-12

### CI/CD
- Add Open VSX publication step to GitHub Actions workflow (`7a5e5a9`)

## [v0.2.2] - 2026-11-12

### Fixes
- Support SQL injection tokenization and matching improvements (`0880833`)

## [v0.2.1] - 2026-11-12

### Refactoring
- Update SQL injection grammar patterns and injection selector (`eb12837`)

## [v0.2.0] - 2026-11-12

### Features
- Add Artisan commands and workspace overview tree views to VS Code extension (`b7ceaa1`)

## [v0.1.0] - 2026-11-12

### Features
- Initialize Goravel VS Code extension and Go language server (`goravel-lsp`) with raw SQL support (`e463489`)
- Add advanced raw SQL diagnostics and code actions for scan structs and placeholders (`4210fb4`)
- Add install script for server to README and `package.json` (`3a1a0b3`)
- Add CI/CD publishing workflow, MIT license, and release documentation (`3bcfd19`)

