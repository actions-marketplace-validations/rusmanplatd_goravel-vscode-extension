import { describe, it, beforeEach } from "node:test";
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

import { registerArtisanCommands, runArtisanInTerminal } from "../src/commands/artisan";

describe("Artisan Commands", () => {
  beforeEach(() => {
    resetMockState();
  });

  function setup(
    customCommands: Array<{ signature: string; description: string; structName: string; loc: { file: string; line: number; col: number } }> = [],
    seeders: Array<{ name: string; signature: string; loc: { file: string; line: number; col: number } }> = []
  ) {
    const ctx = createMockExtensionContext("/ext");
    registerArtisanCommands(
      ctx as unknown as vscode.ExtensionContext,
      () => customCommands,
      () => seeders
    );
    return ctx;
  }

  it("reuses existing Goravel Artisan terminal and respects custom goravel.artisan.command prefix", () => {
    const state = getMockState();
    state.config["goravel.artisan.command"] = "docker compose exec app go run . artisan";

    runArtisanInTerminal("migrate");
    runArtisanInTerminal("route:list");

    assert.equal(state.terminals.length, 1);
    assert.equal(state.terminals[0].name, "Goravel Artisan");
    assert.deepEqual(state.terminals[0].sentTexts, [
      "docker compose exec app go run . artisan migrate",
      "docker compose exec app go run . artisan route:list",
    ]);
    assert.deepEqual(state.terminals[0].shown, [true, true]);
  });

  it("falls back to default prefix when goravel.artisan.command is whitespace", () => {
    const state = getMockState();
    state.config["goravel.artisan.command"] = "   ";
    runArtisanInTerminal("migrate:status");
    assert.deepEqual(state.terminals[0].sentTexts, ["go run . artisan migrate:status"]);
  });

  it("quotes arguments with spaces, single quotes, and backslashes safely on Unix and Windows", async () => {
    setup();
    const state = getMockState();

    // Unix quoting with spaces and single quote
    state.inputBoxQueue.push("My 'Fancy' Model");
    await withMockPlatform("linux", async () => {
      await state.commands.get("goravel.artisan.makeModel")!();
    });
    assert.equal(state.terminals[0].sentTexts[0], `go run . artisan make:model 'My '\\''Fancy'\\'' Model'`);

    // Windows quoting with spaces, backslash, and double quote
    state.inputBoxQueue.push(`Dir\\My "Model"`);
    await withMockPlatform("win32", async () => {
      await state.commands.get("goravel.artisan.makeModel")!();
    });
    assert.equal(state.terminals[0].sentTexts[1], `go run . artisan make:model "Dir\\\\My \\"Model\\""`);

    // Cancellation / empty input does not invoke terminal
    state.inputBoxQueue.push("   ");
    state.inputBoxQueue.push(undefined);
    await state.commands.get("goravel.artisan.makeController")!();
    await state.commands.get("goravel.artisan.makeMigration")!();
    assert.equal(state.terminals[0].sentTexts.length, 2);
  });

  it("executes goravel.artisan.run for custom discovered commands, presets with prompt, presets without prompt, custom input, and cancellation", async () => {
    setup([
      { signature: "users:sync", description: "Sync users", structName: "SyncUsers", loc: { file: "/c.go", line: 1, col: 0 } },
    ]);
    const state = getMockState();
    const runCmd = state.commands.get("goravel.artisan.run")!;

    // 1. Pick discovered custom command
    state.quickPickQueue.push((items: Array<{ cmd: string }>) => items.find((i) => i.cmd === "users:sync"));
    await runCmd();
    assert.equal(state.terminals[0].sentTexts.at(-1), "go run . artisan users:sync");

    // 2. Pick preset with prompt (make:job)
    state.quickPickQueue.push((items: Array<{ cmd: string }>) => items.find((i) => i.cmd === "make:job"));
    state.inputBoxQueue.push("SendInvoice");
    await runCmd();
    assert.equal(state.terminals[0].sentTexts.at(-1), "go run . artisan make:job SendInvoice");

    // 3. Pick preset without prompt (migrate)
    state.quickPickQueue.push((items: Array<{ cmd: string }>) => items.find((i) => i.cmd === "migrate"));
    await runCmd();
    assert.equal(state.terminals[0].sentTexts.at(-1), "go run . artisan migrate");

    // 4. Pick __custom__ command
    state.quickPickQueue.push((items: Array<{ cmd: string }>) => items.find((i) => i.cmd === "__custom__"));
    state.inputBoxQueue.push("cache:clear --force");
    await runCmd();
    assert.equal(state.terminals[0].sentTexts.at(-1), "go run . artisan cache:clear --force");

    // 5. Cancel quick pick
    state.quickPickQueue.push(undefined);
    await runCmd();
    assert.equal(state.terminals[0].sentTexts.length, 4);
  });

  it("registers and executes direct artisan commands (runCommand, migrate, rollback, generators)", async () => {
    setup();
    const state = getMockState();

    state.commands.get("goravel.artisan.runCommand")!("  app:ping  ");
    state.commands.get("goravel.artisan.runCommand")!("   "); // ignored
    state.commands.get("goravel.artisan.migrate")!();
    state.commands.get("goravel.artisan.migrateRollback")!();

    state.inputBoxQueue.push("CreateUserRequest", "AlphaRule", "UserSeeder", "UserEvt", "UserLst", "MailJob", "UserObs", "UserPol", "AppProv", "UserFact", "UserTest");
    for (const id of [
      "goravel.artisan.makeRequest",
      "goravel.artisan.makeRule",
      "goravel.artisan.makeSeeder",
      "goravel.artisan.makeEvent",
      "goravel.artisan.makeListener",
      "goravel.artisan.makeJob",
      "goravel.artisan.makeObserver",
      "goravel.artisan.makePolicy",
      "goravel.artisan.makeProvider",
      "goravel.artisan.makeFactory",
      "goravel.artisan.makeTest",
    ]) {
      await state.commands.get(id)!();
    }

    assert.equal(state.terminals[0].sentTexts[0], "go run . artisan app:ping");
    assert.equal(state.terminals[0].sentTexts[1], "go run . artisan migrate");
    assert.equal(state.terminals[0].sentTexts[2], "go run . artisan migrate:rollback");
    assert.equal(state.terminals[0].sentTexts.at(-1), "go run . artisan make:test UserTest");
  });

  it("handles makeView scaffolding (layout, standalone, CLI, prefill, path traversal guard, no workspace)", async () => {
    setup();
    const state = getMockState();
    const tmpWs = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-view-"));
    try {
      const viewsDir = path.join(tmpWs, "resources", "views", "admin");
      fs.mkdirSync(viewsDir, { recursive: true });
      state.workspaceFolders = [{ uri: MockUri.file(tmpWs), name: "ws" }];

      let refreshed = false;
      state.commands.set("goravel.refreshViews", () => {
        refreshed = true;
      });
      const makeView = state.commands.get("goravel.artisan.makeView")!;

      // 1. Path traversal rejected
      state.inputBoxQueue.push("../../escape.tmpl");
      await makeView(MockUri.file(viewsDir));
      assert.equal(state.notifications.errors.length, 1);
      assert.match(state.notifications.errors[0], /must stay inside resources\/views/);

      // 2. Layout scaffold with auto .tmpl extension
      state.inputBoxQueue.push("admin/dashboard");
      state.quickPickQueue.push((items: Array<{ id: string }>) => items.find((i) => i.id === "layout"));
      await makeView(MockUri.file(viewsDir));
      const dashFile = path.join(tmpWs, "resources", "views", "admin", "dashboard.tmpl");
      assert.ok(fs.existsSync(dashFile));
      assert.match(fs.readFileSync(dashFile, "utf8"), /layouts\/header\.tmpl/);
      assert.equal(refreshed, true);

      // 3. Standalone scaffold
      state.inputBoxQueue.push("cards/item.gohtml");
      state.quickPickQueue.push((items: Array<{ id: string }>) => items.find((i) => i.id === "standalone"));
      await makeView();
      const cardFile = path.join(tmpWs, "resources", "views", "cards", "item.gohtml");
      assert.ok(fs.existsSync(cardFile));
      assert.match(fs.readFileSync(cardFile, "utf8"), /\{\{ define "cards\/item\.gohtml" \}\}/);

      // 4. CLI mode
      state.inputBoxQueue.push("reports/summary.tmpl");
      state.quickPickQueue.push((items: Array<{ id: string }>) => items.find((i) => i.id === "cli"));
      await makeView();
      assert.equal(state.terminals[0].sentTexts.at(-1), "go run . artisan make:view reports/summary.tmpl");

      // 5. No workspace falls back to CLI
      state.workspaceFolders = undefined;
      state.inputBoxQueue.push("plain");
      await makeView();
      assert.equal(state.terminals[0].sentTexts.at(-1), "go run . artisan make:view plain.tmpl");
    } finally {
      fs.rmSync(tmpWs, { recursive: true, force: true });
    }
  });

  it("handles db:seed with discovered seeders and without seeders", async () => {
    const state = getMockState();

    // Without seeders -> InputBox
    setup([], []);
    const seedCmd1 = state.commands.get("goravel.artisan.seedSpecific")!;
    state.inputBoxQueue.push("My Seeder");
    await seedCmd1();
    assert.equal(state.terminals[0].sentTexts.at(-1), `go run . artisan db:seed --seeder='My Seeder'`);
    state.inputBoxQueue.push("");
    await seedCmd1();
    assert.equal(state.terminals[0].sentTexts.at(-1), "go run . artisan db:seed");
    state.inputBoxQueue.push(undefined);
    await seedCmd1();
    assert.equal(state.terminals[0].sentTexts.length, 2);

    // With seeders -> QuickPick
    setup([], [{ name: "UserSeeder", signature: "UserSeeder", loc: { file: "/s.go", line: 0, col: 0 } }]);
    const seedCmd2 = state.commands.get("goravel.artisan.seedSpecific")!;
    state.quickPickQueue.push((items: Array<{ sig: string }>) => items.find((i) => i.sig === "UserSeeder"));
    await seedCmd2();
    assert.equal(state.terminals[0].sentTexts.at(-1), "go run . artisan db:seed --seeder=UserSeeder");
  });

  it("opens goravel.log when present and notifies when missing", async () => {
    setup();
    const state = getMockState();
    const tmpWs = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-log-"));
    try {
      state.workspaceFolders = [{ uri: MockUri.file(tmpWs), name: "ws" }];
      const openLog = state.commands.get("goravel.logs.open")!;

      // Missing log file
      await openLog();
      assert.equal(state.notifications.infos.length, 1);
      assert.match(state.notifications.infos[0], /Log file not found/);

      // Present log file -> opens and scrolls to last line
      const logPath = path.join(tmpWs, "storage", "logs", "goravel.log");
      fs.mkdirSync(path.dirname(logPath), { recursive: true });
      fs.writeFileSync(logPath, "line1\nline2\n", "utf8");
      await openLog();
      assert.equal(state.shownDocs.length, 1);
      assert.equal(state.shownDocs[0].revealedRanges.length, 1);
    } finally {
      fs.rmSync(tmpWs, { recursive: true, force: true });
    }
  });

  it("runs Go tests with APP_ENV=testing when .env.testing exists and handles missing workspace / cancellation", async () => {
    setup();
    const state = getMockState();
    const runTest = state.commands.get("goravel.test.run")!;

    // No workspace -> error notification
    await runTest();
    assert.equal(state.notifications.errors.length, 1);
    assert.match(state.notifications.errors[0], /No workspace folder open/);

    const tmpWs = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-test-"));
    try {
      fs.writeFileSync(path.join(tmpWs, ".env.testing"), "APP_ENV=testing\n", "utf8");
      const pkgDir = path.join(tmpWs, "tests", "feature");
      fs.mkdirSync(pkgDir, { recursive: true });
      state.workspaceFolders = [{ uri: MockUri.file(tmpWs), name: "ws" }];

      // Cancelled input box
      state.inputBoxQueue.push(undefined);
      await runTest();
      assert.equal(state.executedTasks.length, 0);

      // Explicit dir & testName (from CodeLens)
      await runTest(pkgDir, "TestUserFlow");
      assert.equal(state.executedTasks.length, 1);
      const task = state.executedTasks[0];
      assert.equal(task.name, "Go Test: TestUserFlow");
      assert.equal(task.execution.process, "go");
      assert.deepEqual(task.execution.args, ["test", "-v", "-run", "TestUserFlow", `./${path.join("tests", "feature")}`]);
      assert.deepEqual(task.execution.options?.env, { APP_ENV: "testing" });

      // Active .go file with empty filter
      state.activeTextEditor = { document: { uri: MockUri.file(path.join(pkgDir, "user_test.go")) } };
      state.inputBoxQueue.push("");
      await runTest();
      assert.equal(state.executedTasks.length, 2);
      assert.equal(state.executedTasks[1].name, "Go Test");
      assert.deepEqual(state.executedTasks[1].execution.args, ["test", "-v", `./${path.join("tests", "feature")}`]);

      // Non-Go active file runs ./...
      state.activeTextEditor = undefined;
      state.inputBoxQueue.push("");
      await runTest();
      assert.deepEqual(state.executedTasks[2].execution.args, ["test", "-v", "./..."]);
    } finally {
      fs.rmSync(tmpWs, { recursive: true, force: true });
    }
  });
});
