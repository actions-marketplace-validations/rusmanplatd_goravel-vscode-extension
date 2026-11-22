import { describe, it, beforeEach, afterEach } from "node:test";
import * as assert from "node:assert/strict";
import * as fs from "fs";
import * as os from "os";
import * as path from "path";
import type * as vscode from "vscode";
import {
  createMockExtensionContext,
  getMockState,
  installVscodeMock,
  MockUri,
  resetMockState,
  withMockPlatform,
} from "./helpers/vscode-mock";

installVscodeMock();

import { activate, deactivate } from "../src/extension";

describe("Extension Lifecycle & Commands", () => {
  let tmpDir: string;

  beforeEach(() => {
    resetMockState();
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-ext-"));
  });

  afterEach(async () => {
    await deactivate();
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  function makeBundledBin(platform: NodeJS.Platform = process.platform): string {
    const exe = platform === "win32" ? "goravel-lsp.exe" : "goravel-lsp";
    const binPath = path.join(tmpDir, "bin", exe);
    fs.mkdirSync(path.dirname(binPath), { recursive: true });
    fs.writeFileSync(binPath, "#!/bin/sh\n", "utf8");
    return binPath;
  }

  it("does not start language server when goravel.sql.enable is false", async () => {
    const state = getMockState();
    state.config["goravel.sql.enable"] = false;
    makeBundledBin();

    const ctx = createMockExtensionContext(tmpDir);
    await activate(ctx as unknown as vscode.ExtensionContext);

    assert.equal(state.languageClients.length, 0);
    assert.equal(state.treeProviders.size, 5);
    assert.equal(state.statusBars.length, 1);
    assert.equal(state.watchers.length, 0);
  });

  it("shows actionable error when no bundled or custom server binary exists", async () => {
    const state = getMockState();
    const ctx = createMockExtensionContext(tmpDir);
    await activate(ctx as unknown as vscode.ExtensionContext);

    assert.equal(state.languageClients.length, 0);
    assert.equal(state.notifications.errors.length, 1);
    assert.match(state.notifications.errors[0], /goravel-lsp binary not found/);
  });

  it("prefers existing custom goravel.server.path and resolves Windows .exe bundled binary", async () => {
    const state = getMockState();
    const customBin = path.join(tmpDir, "custom-lsp");
    fs.writeFileSync(customBin, "bin", "utf8");
    state.config["goravel.server.path"] = customBin;
    state.config["goravel.sql.liveIntrospection"] = true;
    state.config["goravel.sql.source"] = "both";

    const ctx = createMockExtensionContext(tmpDir);
    await activate(ctx as unknown as vscode.ExtensionContext);

    assert.equal(state.languageClients.length, 1);
    const lc = state.languageClients[0];
    assert.equal((lc.serverOptions as { command: string }).command, customBin);
    assert.deepEqual((lc.clientOptions as { initializationOptions: unknown }).initializationOptions, {
      liveIntrospection: true,
      sqlSource: "both",
    });
    assert.equal(lc.started, true);
    assert.equal(state.watchers.length, 1);

    await deactivate();
    assert.equal(lc.stopped, true);
    assert.equal(state.watchers[0].disposed, true);

    // Now test Windows .exe resolution when custom path does not exist
    resetMockState();
    const winState = getMockState();
    winState.config["goravel.server.path"] = path.join(tmpDir, "missing-custom");
    const winBin = makeBundledBin("win32");
    await withMockPlatform("win32", async () => {
      const ctx2 = createMockExtensionContext(tmpDir);
      await activate(ctx2 as unknown as vscode.ExtensionContext);
    });
    assert.equal(winState.languageClients.length, 1);
    assert.equal((winState.languageClients[0].serverOptions as { command: string }).command, winBin);
  });

  it("handles overview population, status bar updates, config change notifications, restart, and server errors", async () => {
    const state = getMockState();
    makeBundledBin();
    state.requestHandler = async (method) => {
      if (method === "goravel/overview") {
        return {
          tables: [{ name: "users", model: "User", columns: [{ name: "id", goName: "ID", goType: "uint", sqlType: "BIGINT", doc: "", nullable: false, loc: { file: "/u.go", line: 1, col: 0 }, migLoc: { file: "/m.go", line: 1, col: 0 } }], loc: { file: "/u.go", line: 1, col: 0 }, migLoc: { file: "/m.go", line: 1, col: 0 } }],
          routes: [{ method: "POST", path: "/users/{id}", handler: "UserController.Update", controller: "UserController", action: "Update", module: "api", loc: { file: "/r.go", line: 4, col: 0 }, ctrlLoc: { file: "/c.go", line: 8, col: 0 } }],
          migrations: [{ name: "m1", folder: "migrations", table: "users", loc: { file: "/m.go", line: 0, col: 0 } }],
          commands: [{ signature: "app:run", description: "Run", structName: "RunCmd", loc: { file: "/cmd.go", line: 0, col: 0 } }],
          seeders: [],
          schedules: [],
          views: [],
          unregistered: [],
        };
      }
      if (method === "goravel/routeSample") {
        return { body: `{\n  "name": "Alice"\n}` };
      }
      return undefined;
    };

    const ctx = createMockExtensionContext(tmpDir);
    await activate(ctx as unknown as vscode.ExtensionContext);

    assert.equal(state.statusBars[0].shown, true);
    assert.match(state.statusBars[0].text, /1 tables · 1 routes/);

    // Configuration change triggers workspace/didChangeConfiguration notification
    state.config["goravel.sql.liveIntrospection"] = true;
    state.config["goravel.sql.source"] = "live-db";
    for (const listener of state.configListeners) {
      listener({ affectsConfiguration: (s) => s === "goravel.sql.source" });
    }
    const lc1 = state.languageClients[0];
    assert.equal(lc1.notifications.length, 1);
    assert.equal(lc1.notifications[0].method, "workspace/didChangeConfiguration");
    assert.deepEqual(lc1.notifications[0].params, {
      settings: { goravel: { sql: { liveIntrospection: true, source: "live-db" } } },
    });

    // Restart stops old client, disposes old watcher, and starts a new client
    await state.commands.get("goravel.restartServer")!();
    assert.equal(lc1.stopped, true);
    assert.equal(state.watchers[0].disposed, true);
    assert.equal(state.languageClients.length, 2);
    assert.equal(state.languageClients[1].started, true);

    // Overview request error is swallowed gracefully during refreshViews
    state.requestHandler = async () => {
      throw new Error("server restarting");
    };
    await assert.doesNotReject(async () => {
      await state.commands.get("goravel.refreshViews")!();
    });
  });

  it("propagates startup failure cleanly when LanguageClient.start rejects", async () => {
    const state = getMockState();
    makeBundledBin();
    state.nextClientStartError = new Error("spawn EACCES");
    const ctx = createMockExtensionContext(tmpDir);
    await assert.rejects(() => activate(ctx as unknown as vscode.ExtensionContext), /spawn EACCES/);
  });

  it("handles file opening, file creation, route testing, migration opening, table preview, and bootstrap registration", async () => {
    const state = getMockState();
    makeBundledBin();

    let bootstrapResponse: unknown = { ok: true, title: "Register M1 in bootstrap/migrations.go", targetFile: "/boot/migrations.go" };
    state.requestHandler = async (method, params) => {
      if (method === "goravel/routeSample") {
        if ((params as { action?: string }).action === "Fail") throw new Error("timeout");
        return { body: `{\n  "email": ""\n}` };
      }
      if (method === "goravel/registerBootstrap") return bootstrapResponse;
      if (method === "goravel/explainQuery") return { ok: true, output: "Seq Scan on users" };
      if (method === "goravel/previewQuery") {
        const sql = (params as { sql: string }).sql;
        if (sql.includes("bad_json")) return { ok: true, output: "{not json" };
        if (sql.includes("fail_me")) return { ok: false, output: "", error: "permission denied" };
        return { ok: true, output: JSON.stringify([{ id: 1, name: "<script>alert(1)</script>", bio: null }]) };
      }
      if (method === "goravel/erDiagram") return { mermaid: "erDiagram\n  users {\n    bigint id PK\n  }\n" };
      if (method === "goravel/overview") {
        return {
          tables: [{ name: "users", model: "User", columns: [{ name: "id", goName: "ID", goType: "uint", sqlType: "BIGINT", doc: "", nullable: false, foreignKey: "", loc: { file: "/u.go", line: 2, col: 0 }, migLoc: { file: "/m.go", line: 4, col: 0 } }], loc: { file: "/u.go", line: 2, col: 0 }, migLoc: { file: "/m.go", line: 4, col: 0 } }],
          routes: [{ method: "GET", path: "/users", handler: "UserController.Index", controller: "UserController", action: "Index", module: "api", middlewares: ["Auth"], loc: { file: "/r.go", line: 3, col: 0 }, ctrlLoc: { file: "/c.go", line: 5, col: 0 } }],
          migrations: [],
        };
      }
      return undefined;
    };

    const ctx = createMockExtensionContext(tmpDir);
    await activate(ctx as unknown as vscode.ExtensionContext);

    // openLocation & jumpToRoute
    await state.commands.get("goravel.openLocation")!("/app/a.go", 3, 2);
    await state.commands.get("goravel.jumpToRoute")!("file:///routes/api.go", 5, 0);
    assert.equal(state.shownDocs.length, 2);

    // createFileIfMissing
    const newFile = path.join(tmpDir, "resources", "views", "new.tmpl");
    await state.commands.get("goravel.createFileIfMissing")!(newFile, "{{ define \"new.tmpl\" }}{{ end }}");
    assert.equal(fs.readFileSync(newFile, "utf8"), "{{ define \"new.tmpl\" }}{{ end }}");

    // route.testRequest (POST with sample body & ANY -> GET)
    await state.commands.get("goravel.route.testRequest")!({ method: "POST", path: "/users/{id}", controller: "UserController", action: "Store" });
    const httpDoc = state.openedDocs.at(-1)!;
    assert.equal(httpDoc.language, "http");
    assert.match(httpDoc.content || "", /POST \{\{host\}\}\/users\/\{\{id\}\}/);
    assert.match(httpDoc.content || "", /"email": ""/);

    await state.commands.get("goravel.route.testRequest")!({ method: "PUT", path: "/users/{id}", controller: "UserController", action: "Fail" });
    assert.match(state.openedDocs.at(-1)!.content || "", /PUT \{\{host\}\}\/users\/\{\{id\}\}/);
    assert.match(state.openedDocs.at(-1)!.content || "", /\{\n  \n\}/);

    await state.commands.get("goravel.route.testRequest")!({ route: { method: "ANY", path: "/ping" } });
    assert.match(state.openedDocs.at(-1)!.content || "", /GET \{\{host\}\}\/ping/);

    // route.openDefinition & model.openMigration (present and missing)
    await state.commands.get("goravel.route.openDefinition")!({ route: { loc: { file: "/routes/api.go", line: 1, col: 0 } } });
    await state.commands.get("goravel.model.openMigration")!({ table: { migLoc: { file: "/mig.go", line: 2, col: 0 } } });
    await state.commands.get("goravel.model.openMigration")!({ table: {} });
    assert.match(state.notifications.infos.at(-1) || "", /No migration file found/);

    // table.preview quotes special identifiers and rejects semicolon injection
    const reqCountBefore = state.languageClients[0].requests.length;
    await state.commands.get("goravel.table.preview")!({ table: { name: "users; DROP TABLE users" } });
    assert.equal(state.languageClients[0].requests.length, reqCountBefore);

    await state.commands.get("goravel.table.preview")!({ table: { name: "my-table" } });
    const previewReq = state.languageClients[0].requests.find((r) => r.method === "goravel/previewQuery");
    assert.deepEqual(previewReq?.params, { sql: `SELECT * FROM "my-table" LIMIT 20` });
    assert.equal(state.webviews.length, 1);
    assert.match(state.webviews[0].webview.html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
    assert.match(state.webviews[0].webview.html, /<td>NULL<\/td>/);

    // previewQuery reuse, bad JSON, error, and dispose
    await state.commands.get("goravel.previewQuery")!("SELECT * FROM bad_json");
    assert.equal(state.webviews[0].revealed.length, 1);
    assert.match(state.webviews[0].webview.html, /0 rows returned/);
    await state.commands.get("goravel.previewQuery")!("SELECT * FROM fail_me");
    assert.match(state.notifications.errors.at(-1) || "", /permission denied/);
    state.webviews[0].dispose();

    // explainQuery
    await state.commands.get("goravel.explainQuery")!("SELECT * FROM users");
    assert.equal(state.openedDocs.at(-1)!.language, "sql");
    assert.match(state.openedDocs.at(-1)!.content || "", /Seq Scan on users/);

    // registerBootstrap: regression test for Bug #3 (server-written response without edit.changes)
    await state.commands.get("goravel.registerBootstrap")!("/database/migrations/m1.go");
    assert.match(state.notifications.infos.at(-1) || "", /Register M1/);
    assert.equal(state.notifications.warnings.length, 0);

    // registerBootstrap: response with workspace edit.changes applies and saves
    bootstrapResponse = {
      ok: true,
      title: "Applied edit",
      edit: {
        changes: {
          "file:///bootstrap/migrations.go": [
            { range: { start: { line: 2, character: 0 }, end: { line: 2, character: 0 } }, newText: "&M1{}," },
          ],
        },
      },
    };
    await state.commands.get("goravel.registerBootstrap")!({ item: { file: "/m1.go" } });
    assert.equal(state.appliedEdits.length, 1);
    assert.equal(state.openedDocs.at(-1)!.saved, true);

    // registerBootstrap: error response warns
    bootstrapResponse = { ok: false, error: "already registered" };
    await state.commands.get("goravel.registerBootstrap")!("/m1.go");
    assert.match(state.notifications.warnings.at(-1) || "", /already registered/);

    // showErDiagram & showRouteMatrix webviews + message handling + reuse + dispose
    await state.commands.get("goravel.showErDiagram")!();
    const erPanel = state.webviews.find((w) => w.viewType === "goravelErDiagram")!;
    assert.match(erPanel.webview.html, /Goravel Schema Overview \(1 tables\)/);
    await erPanel.webview.emitMessage({ type: "open", file: "/u.go", line: 2 });
    await state.commands.get("goravel.showErDiagram")!();
    assert.equal(erPanel.revealed.length, 1);
    erPanel.dispose();

    await state.commands.get("goravel.showRouteMatrix")!();
    const rmPanel = state.webviews.find((w) => w.viewType === "goravelRouteMatrix")!;
    assert.match(rmPanel.webview.html, /Goravel Route Matrix \(1 routes\)/);
    await rmPanel.webview.emitMessage({ type: "open", file: "/r.go", line: 3 });
    await rmPanel.webview.emitMessage({ type: "test", route: { method: "GET", path: "/users" } });
    await state.commands.get("goravel.showRouteMatrix")!();
    assert.equal(rmPanel.revealed.length, 1);
    rmPanel.dispose();
  });

  it("debounces overview refresh on workspace Go/template/.env saves and ignores unrelated files", async () => {
    const state = getMockState();
    makeBundledBin();
    state.workspaceFolders = [{ uri: MockUri.file(tmpDir), name: "ws" }];

    let overviewCalls = 0;
    state.requestHandler = async (method) => {
      if (method === "goravel/overview") {
        overviewCalls++;
        return { tables: [], routes: [], migrations: [] };
      }
      return undefined;
    };

    const ctx = createMockExtensionContext(tmpDir);
    await activate(ctx as unknown as vscode.ExtensionContext);
    assert.equal(overviewCalls, 1);

    // Unrelated extension and non-workspace file should NOT trigger refresh
    for (const cb of state.saveListeners) {
      cb({ uri: MockUri.file(path.join(tmpDir, "README.md")) });
      cb({ uri: MockUri.file("/outside/other.go") });
    }
    await new Promise((r) => setTimeout(r, 350));
    assert.equal(overviewCalls, 1);

    // Rapid saves of .go and .env should debounce into a single refresh
    for (const cb of state.saveListeners) {
      cb({ uri: MockUri.file(path.join(tmpDir, "app/models/user.go")) });
      cb({ uri: MockUri.file(path.join(tmpDir, ".env")) });
    }
    await new Promise((r) => setTimeout(r, 380));
    assert.equal(overviewCalls, 2);

    // Save followed by immediate deactivate clears pending timer
    for (const cb of state.saveListeners) {
      cb({ uri: MockUri.file(path.join(tmpDir, "resources/views/a.tmpl")) });
    }
    await deactivate();
    await new Promise((r) => setTimeout(r, 350));
    assert.equal(overviewCalls, 2);
  });
});
