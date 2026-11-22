package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
	"github.com/rusmanplatd/goravel-lsp/internal/lsp"
)

var version = "0.7.1"

func parseSQLSourceFlag(args []string, i int) (src indexer.SQLSource, nextIdx int, handled bool, err error) {
	arg := args[i]
	switch {
	case arg == "--live-db":
		return indexer.SQLSourceLiveDB, i, true, nil
	case arg == "--sql-source":
		if i+1 >= len(args) {
			return "", i, true, fmt.Errorf("goravel-lsp: --sql-source requires a value (auto, model, live-db, both)")
		}
		v, ok := indexer.ParseSQLSource(args[i+1])
		if !ok {
			return "", i + 1, true, fmt.Errorf("goravel-lsp: invalid --sql-source %q (expected auto, model, live-db, or both)", args[i+1])
		}
		return v, i + 1, true, nil
	case strings.HasPrefix(arg, "--sql-source="):
		val := strings.TrimPrefix(arg, "--sql-source=")
		v, ok := indexer.ParseSQLSource(val)
		if !ok {
			return "", i, true, fmt.Errorf("goravel-lsp: invalid --sql-source %q (expected auto, model, live-db, or both)", val)
		}
		return v, i, true, nil
	default:
		return "", i, false, nil
	}
}

func runCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	sqlSource := indexer.SQLSourceAuto
	if len(args) > 0 {
		cmd := args[0]
		switch cmd {
		case "version", "--version", "-v":
			fmt.Fprintf(stdout, "goravel-lsp version %s\n", version)
			return 0
		case "--dump", "dump":
			root := "."
			if len(args) > 1 {
				root = args[1]
			}
			cat := indexer.Build(root)
			for _, n := range cat.TableNames() {
				t := cat.Table(n)
				fmt.Fprintf(stdout, "%s (%s) cols=%d rels=%d\n", t.Name, t.Model, len(t.Columns), len(t.Relations))
			}
			fmt.Fprintf(stdout, "\nSummary: %d tables, %d routes, %d configs, %d rules, %d migrations\n",
				len(cat.Tables), len(cat.Routes), len(cat.Configs), len(cat.Rules), len(cat.Migrations))
			return 0
		case "run", "check", "--check", "--check-sql", "--check-templates":
			opts := lsp.CheckOptions{
				Root:          ".",
				OnlySQL:       cmd == "--check-sql",
				OnlyTemplates: cmd == "--check-templates",
				Stdout:        stdout,
			}
			rest := args[1:]
			for i := 0; i < len(rest); i++ {
				src, ni, ok, err := parseSQLSourceFlag(rest, i)
				if err != nil {
					fmt.Fprintln(stderr, err)
					return 2
				}
				if ok {
					opts.SQLSource = src
					i = ni
					continue
				}
				arg := rest[i]
				switch {
				case arg == "--only-sql" || arg == "--sql" || arg == "--check-sql":
					opts.OnlySQL = true
				case arg == "--only-templates" || arg == "--templates" || arg == "--check-templates":
					opts.OnlyTemplates = true
				case arg == "--all":
					opts.OnlySQL = false
					opts.OnlyTemplates = false
				case arg == "--errors-only":
					opts.ErrorsOnly = true
				case strings.HasPrefix(arg, "--out-format="):
					opts.OutFormat = strings.TrimPrefix(arg, "--out-format=")
				case strings.HasPrefix(arg, "--skip-dirs="):
					opts.SkipDirs = append(opts.SkipDirs, strings.Split(strings.TrimPrefix(arg, "--skip-dirs="), ",")...)
				case !strings.HasPrefix(arg, "-"):
					opts.Root = arg
				}
			}
			return lsp.CheckWithOptions(opts)
		default:
			for i := 0; i < len(args); i++ {
				src, ni, ok, err := parseSQLSourceFlag(args, i)
				if err != nil {
					fmt.Fprintln(stderr, err)
					return 2
				}
				if ok {
					sqlSource = src
					i = ni
				}
			}
		}
	}
	srv := lsp.New(stdin, stdout)
	srv.SetSQLSource(sqlSource)
	if err := srv.Run(); err != nil {
		fmt.Fprintln(stderr, "goravel-lsp:", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
