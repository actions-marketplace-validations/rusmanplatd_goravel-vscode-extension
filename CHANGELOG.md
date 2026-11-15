# Changelog

All notable changes to the **Goravel** VS Code extension (`goravel-vscode`) and `goravel-lsp` are documented in this file, organized by git release tag.

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

