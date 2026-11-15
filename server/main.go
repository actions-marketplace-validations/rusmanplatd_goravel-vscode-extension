package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
	"github.com/rusmanplatd/goravel-lsp/internal/lsp"
)

var version = "0.7.1"

func main() {
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
			for _, arg := range os.Args[2:] {
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
		}
	}
	if err := lsp.New(os.Stdin, os.Stdout).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "goravel-lsp:", err)
		os.Exit(1)
	}
}
