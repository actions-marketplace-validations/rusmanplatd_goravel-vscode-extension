import { describe, it } from "node:test";
import * as assert from "node:assert/strict";
import * as fs from "fs";
import * as path from "path";

const repoRoot = path.resolve(__dirname, "../..");

function readJSON(relPath: string): Record<string, unknown> {
  const full = path.join(repoRoot, relPath);
  return JSON.parse(fs.readFileSync(full, "utf8")) as Record<string, unknown>;
}

describe("Extension Configuration, Grammars, Snippets, and Consistency", () => {
  const pkg = readJSON("package.json") as {
    main: string;
    activationEvents: string[];
    contributes: {
      languages: Array<{ id: string; extensions: string[]; configuration: string }>;
      configuration: { properties: Record<string, { type: string; default: unknown; enum?: string[] }> };
      viewsContainers: { activitybar: Array<{ id: string; icon: string }> };
      views: Record<string, Array<{ id: string; name: string }>>;
      commands: Array<{ command: string; title: string }>;
      menus: Record<string, Array<{ command: string; when?: string }>>;
      snippets: Array<{ language: string; path: string }>;
      grammars: Array<{ language?: string; scopeName: string; path: string; injectTo?: string[] }>;
    };
  };

  const extSrc = fs.readFileSync(path.join(repoRoot, "client/src/extension.ts"), "utf8");
  const artisanSrc = fs.readFileSync(path.join(repoRoot, "client/src/commands/artisan.ts"), "utf8");
  const explorersSrc = fs.readFileSync(path.join(repoRoot, "client/src/views/explorers.ts"), "utf8");
  const allClientSrc = extSrc + "\n" + artisanSrc + "\n" + explorersSrc;

  it("validates package.json contributions and referenced resource files exist", () => {
    assert.equal(pkg.main, "./dist/extension.js");
    assert.ok(pkg.activationEvents.includes("onLanguage:goravel-tmpl"));

    for (const container of pkg.contributes.viewsContainers.activitybar) {
      assert.ok(fs.existsSync(path.join(repoRoot, container.icon)), `Missing icon: ${container.icon}`);
    }
    for (const lang of pkg.contributes.languages) {
      assert.ok(fs.existsSync(path.join(repoRoot, lang.configuration)), `Missing language config: ${lang.configuration}`);
    }
    for (const snip of pkg.contributes.snippets) {
      assert.ok(fs.existsSync(path.join(repoRoot, snip.path)), `Missing snippet file: ${snip.path}`);
    }
    for (const gram of pkg.contributes.grammars) {
      assert.ok(fs.existsSync(path.join(repoRoot, gram.path)), `Missing grammar file: ${gram.path}`);
    }
  });

  it("ensures every contributed command and menu command is registered in client/src", () => {
    const registered = new Set<string>();
    for (const match of allClientSrc.matchAll(/registerCommand\(\s*"([^"]+)"/g)) {
      registered.add(match[1]);
    }
    const contributed = new Set<string>();
    for (const cmd of pkg.contributes.commands) {
      assert.ok(!contributed.has(cmd.command), `Duplicate contributed command: ${cmd.command}`);
      contributed.add(cmd.command);
      assert.ok(registered.has(cmd.command), `Contributed command ${cmd.command} is not registered in client/src`);
    }
    for (const [menuName, entries] of Object.entries(pkg.contributes.menus)) {
      for (const entry of entries) {
        assert.ok(
          contributed.has(entry.command),
          `Menu ${menuName} references uncontributed command ${entry.command}`
        );
      }
    }
  });

  it("ensures every contributed view, contextValue, setting, and language matches implementation", () => {
    // Views registered in extension.ts
    for (const views of Object.values(pkg.contributes.views)) {
      for (const v of views) {
        assert.ok(
          extSrc.includes(`registerTreeDataProvider("${v.id}"`),
          `Contributed view ${v.id} is not registered in extension.ts`
        );
      }
    }

    // Settings read in client/src
    for (const key of Object.keys(pkg.contributes.configuration.properties)) {
      const shortKey = key.replace(/^goravel\./, "");
      assert.ok(
        allClientSrc.includes(`"${shortKey}"`) || allClientSrc.includes(`"${key}"`),
        `Setting ${key} is not referenced in client/src`
      );
    }

    // Menu viewItem contextValues match explorers.ts
    for (const entry of pkg.contributes.menus["view/item/context"] || []) {
      const exactMatch = entry.when?.match(/viewItem\s*==\s*(\w+)/);
      if (exactMatch) {
        assert.ok(
          explorersSrc.includes(`"${exactMatch[1]}"`),
          `Menu viewItem ${exactMatch[1]} not found in explorers.ts`
        );
      }
    }

    // Language ID and extensions match extension.ts documentSelector
    const tmplLang = pkg.contributes.languages.find((l) => l.id === "goravel-tmpl");
    assert.ok(tmplLang, "goravel-tmpl language missing");
    assert.deepEqual(tmplLang.extensions, [".tmpl", ".gohtml"]);
    assert.ok(extSrc.includes(`language: "goravel-tmpl"`));
    assert.ok(extSrc.includes(`**/*.{tmpl,gohtml}`));
  });

  it("validates language-configuration.tmpl.json structure and regexes", () => {
    const cfg = readJSON("language-configuration.tmpl.json") as {
      comments: { blockComment: [string, string] };
      brackets: Array<[string, string]>;
      autoClosingPairs: Array<{ open: string; close: string }>;
      folding: { markers: { start: string; end: string } };
      wordPattern: string;
    };
    assert.deepEqual(cfg.comments.blockComment, ["{{/*", "*/}}"]);
    assert.ok(cfg.brackets.length >= 4);
    assert.ok(cfg.autoClosingPairs.some((p) => p.open === "{{" && p.close === "}}"));
    assert.doesNotThrow(() => new RegExp(cfg.folding.markers.start));
    assert.doesNotThrow(() => new RegExp(cfg.folding.markers.end));
    assert.doesNotThrow(() => new RegExp(cfg.wordPattern));
  });

  it("validates TextMate grammar JSON files and repository references", () => {
    for (const gramDecl of pkg.contributes.grammars) {
      const gram = readJSON(gramDecl.path) as {
        scopeName: string;
        patterns: Array<{ include?: string }>;
        repository: Record<string, unknown>;
      };
      assert.equal(gram.scopeName, gramDecl.scopeName, `scopeName mismatch in ${gramDecl.path}`);
      assert.ok(gram.repository && typeof gram.repository === "object");

      // Check every internal "#ref" include points to a defined repository key
      const raw = JSON.stringify(gram);
      for (const m of raw.matchAll(/"include":"#([^"]+)"/g)) {
        assert.ok(
          Object.prototype.hasOwnProperty.call(gram.repository, m[1]),
          `Grammar ${gramDecl.path} references missing repository rule #${m[1]}`
        );
      }
    }
  });

  it("validates snippet files have non-empty unique prefixes and bodies", () => {
    for (const relPath of ["snippets/goravel.code-snippets", "snippets/goravel-tmpl.code-snippets"]) {
      const snips = readJSON(relPath) as Record<
        string,
        { prefix: string | string[]; body: string | string[]; description?: string }
      >;
      const keys = Object.keys(snips);
      assert.ok(keys.length > 0, `Snippet file ${relPath} is empty`);
      const prefixes = new Set<string>();
      for (const name of keys) {
        const s = snips[name];
        const pList = Array.isArray(s.prefix) ? s.prefix : [s.prefix];
        assert.ok(pList.length > 0 && pList.every((p) => typeof p === "string" && p.length > 0), `Invalid prefix on ${name}`);
        for (const p of pList) {
          assert.ok(!prefixes.has(p), `Duplicate snippet prefix ${p} in ${relPath}`);
          prefixes.add(p);
        }
        const bodyLines = Array.isArray(s.body) ? s.body : [s.body];
        assert.ok(bodyLines.length > 0, `Empty body on snippet ${name}`);
      }
    }
  });
});
