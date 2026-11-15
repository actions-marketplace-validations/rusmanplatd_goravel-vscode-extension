# Goravel for VS Code (`goravel-vscode`)

Rich IDE tooling, Language Server (`goravel-lsp`), Raw SQL & ORM intelligence, Go HTML Template (`.tmpl` / `.gohtml`) support, Workspace Explorers, and Artisan CLI integration for [Goravel](https://www.goravel.dev/) projects.

## Features

### Go & Raw SQL Intelligence (`goravel-lsp`)
- **Embedded Raw SQL Highlighting**: TextMate injection grammar highlights SQL inside Go strings for Goravel query methods.
- **SQL Diagnostics, Hover, Completion & Navigation**:
  - Schema-aware validation for tables, columns, column aliases, CTEs, subqueries, and table name variables.
  - Multi-dialect SQL support with optional live database schema introspection via `.env` `DB_*` settings.
  - Diagnostics and code actions for scan structs and SQL placeholders.
- **ORM & Framework Intelligence**:
  - Model, relationship, config, route (including gRPC routes), observer, validation rule, and migration indexing.
  - `facades.View` intelligence linking Go handlers to template files and view data.

### Goravel Template Support (`goravel-tmpl`)
- Syntax highlighting and language configuration for `.tmpl` and `.gohtml` files.
- LSP hover with span tracking, detailed type resolution, and code snippets for Go/Goravel templates.

### Goravel Activity Bar Explorer
Dedicated sidebar views under the **Goravel** activity bar container:
- **Models & Schema** — Browse indexed models, tables, and columns; preview top 20 table rows, jump to table migrations, or open an **ER Diagram**.
- **Routes** — Browse HTTP and gRPC routes, open route registrations, test route requests, or view the **Route Matrix**.
- **Views & Templates** — Explore `.tmpl`/`.gohtml` views and scaffold new views with `make:view`.
- **Migrations** — View live migration status (applied/pending) and run `migrate` or `migrate:rollback` inline.
- **Commands, Seeders & Schedules** — Inspect custom Artisan commands, seeders, and scheduled tasks, and register unregistered items in `bootstrap/`.

### Artisan CLI & Productivity
- Context menu integration in the Explorer for `app/models`, `app/http/controllers`, `app/http/requests`, `app/rules`, `app/events`, `app/listeners`, `app/jobs`, `app/observers`, `app/policies`, `app/providers`, `database/migrations`, `database/seeders`, `database/factories`, `tests`, and `resources/views`.
- Command Palette support for `make:*` generators, `migrate`, `migrate:rollback`, `db:seed...`, running Go tests, and opening `storage/logs/goravel.log`.
- Built-in code snippets for Go (`snippets/goravel.code-snippets`) and Goravel templates (`snippets/goravel-tmpl.code-snippets`).

## Installation

### VS Code Extension
Install **Goravel** (`rusmanplatd.goravel-vscode`) from the VS Code Marketplace or Open VSX Registry, or install from a `.vsix` package:

```bash
code --install-extension goravel-vscode-0.8.1.vsix
```

The extension activates automatically when your workspace contains `artisan`, `app/models`, or when opening a `.tmpl`/`.gohtml` file (requires VS Code `^1.85.0`).

### Standalone `goravel-lsp` CLI
You can install the standalone `goravel-lsp` binary using [`install.sh`](install.sh):

```bash
curl -sSfL https://raw.githubusercontent.com/rusmanplatd/goravel-vscode-extension/main/install.sh | sh -s -- -b /usr/local/bin
```

Or run it directly against a Goravel project:

```bash
goravel-lsp --check-sql .
goravel-lsp --dump .
```

Available CLI flags for `run` / `check` / `--check` / `--check-sql`:
- `--only-sql` / `--sql` / `--check-sql` — Check only SQL and ORM database queries
- `--all` — Run all diagnostics
- `--errors-only` — Report errors only
- `--out-format=github-actions` — Format output for GitHub Actions annotations
- `--skip-dirs=dir1,dir2` — Comma-separated directories to skip

### GitHub Action
Use `goravel-lsp` in CI workflows via [`action.yml`](action.yml) to lint SQL and Goravel ORM queries:

```yaml
- uses: rusmanplatd/goravel-vscode-extension@v0.8.1
  with:
    version: "v0.8.1"        # 'latest', tag (e.g. 'v0.8.1'), or 'main'
    working-directory: "."
    args: "--check-sql"
    only-sql: "true"
```

## Extension Settings

| Setting | Default | Description |
| --- | --- | --- |
| `goravel.server.path` | `""` | Path to the `goravel-lsp` binary. Defaults to the bundled `bin/goravel-lsp`. |
| `goravel.sql.enable` | `true` | Enable Goravel Raw SQL and Framework intelligence inside Go files. |
| `goravel.sql.liveIntrospection` | `false` | Enable optional live database schema introspection using `.env` `DB_*` settings. |
| `goravel.artisan.command` | `"go run . artisan"` | Base Artisan CLI command prefix (e.g. `go run . artisan`, `./artisan`, or `docker compose exec app go run . artisan`). |

## Development

```bash
# Install Node dependencies
npm install

# Build Go LSP server (outputs to bin/goravel-lsp) and bundle TS client
npm run build

# Run Go server tests and TypeScript typecheck
npm run test:server
npm run typecheck

# Package .vsix into dist/
npm run package
```

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for release notes across all git tags.

## License

[MIT](LICENSE) © 2026 rusmanplatd

