package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
	"github.com/rusmanplatd/goravel-lsp/internal/lsp"
)

var version = "0.7.1"

func parseSQLSourceFlag(args []string, i int) (src indexer.SQLSource, nextIdx int, handled bool) {
	arg := args[i]
	switch {
	case arg == "--live-db":
		return indexer.SQLSourceLiveDB, i, true
	case arg == "--sql-source":
		if i+1 >= len(args) {
			fmt.Fprintln(os.Stderr, "goravel-lsp: --sql-source requires a value (auto, model, live-db, both)")
			os.Exit(2)
		}
		v, ok := indexer.ParseSQLSource(args[i+1])
		if !ok {
			fmt.Fprintf(os.Stderr, "goravel-lsp: invalid --sql-source %q (expected auto, model, live-db, or both)\n", args[i+1])
			os.Exit(2)
		}
		return v, i + 1, true
	case strings.HasPrefix(arg, "--sql-source="):
		val := strings.TrimPrefix(arg, "--sql-source=")
		v, ok := indexer.ParseSQLSource(val)
		if !ok {
			fmt.Fprintf(os.Stderr, "goravel-lsp: invalid --sql-source %q (expected auto, model, live-db, or both)\n", val)
			os.Exit(2)
		}
		return v, i, true
	default:
		return "", i, false
	}
}

func main() {
	sqlSource := indexer.SQLSourceAuto
	if len(os.Args) > 1 {
		cmd := os.Args[1]
		switch cmd {
		case "version", "--version", "-v":
			fmt.Printf("goravel-lsp version %s\n", version)
			return
		case "--dump", "dump":
			root := "."
			if len(os.Args) > 2 {
				root = os.Args[2]
			}
			cat := indexer.Build(root)
			for _, n := range cat.TableNames() {
				t := cat.Table(n)
				fmt.Printf("%s (%s) cols=%d rels=%d\n", t.Name, t.Model, len(t.Columns), len(t.Relations))
			}
			fmt.Printf("\nSummary: %d tables, %d routes, %d configs, %d rules, %d migrations\n",
				len(cat.Tables), len(cat.Routes), len(cat.Configs), len(cat.Rules), len(cat.Migrations))
			return
		case "run", "check", "--check", "--check-sql":
			opts := lsp.CheckOptions{
				Root:    ".",
				OnlySQL: cmd == "--check-sql",
			}
			args := os.Args[2:]
			for i := 0; i < len(args); i++ {
				if src, ni, ok := parseSQLSourceFlag(args, i); ok {
					opts.SQLSource = src
					i = ni
					continue
				}
				arg := args[i]
				switch {
				case arg == "--only-sql" || arg == "--sql" || arg == "--check-sql":
					opts.OnlySQL = true
				case arg == "--all":
					opts.OnlySQL = false
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
			os.Exit(lsp.CheckWithOptions(opts))
		default:
			args := os.Args[1:]
			for i := 0; i < len(args); i++ {
				if src, ni, ok := parseSQLSourceFlag(args, i); ok {
					sqlSource = src
					i = ni
				}
			}
		}
	}
	srv := lsp.New(os.Stdin, os.Stdout)
	srv.SetSQLSource(sqlSource)
	if err := srv.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "goravel-lsp:", err)
		os.Exit(1)
	}
}
