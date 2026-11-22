import { describe, it, beforeEach } from "node:test";
import * as assert from "node:assert/strict";
import { installVscodeMock, resetMockState, MockThemeIcon } from "./helpers/vscode-mock";

installVscodeMock();

import {
  CommandsTreeProvider,
  MigrationsTreeProvider,
  ModelsTreeProvider,
  RoutesTreeProvider,
  ViewsTreeProvider,
  type TableInfo,
} from "../src/views/explorers";

describe("Explorer TreeDataProviders", () => {
  beforeEach(() => {
    resetMockState();
  });

  describe("ModelsTreeProvider", () => {
    it("handles empty and missing data gracefully", () => {
      const provider = new ModelsTreeProvider();
      assert.deepEqual(provider.getChildren(), []);
      let fired = 0;
      provider.onDidChangeTreeData(() => {
        fired++;
      });
      provider.setData(undefined as unknown as TableInfo[]);
      assert.equal(fired, 1);
      assert.deepEqual(provider.getChildren(), []);
    });

    it("renders tables, columns, foreign keys, and named relations with navigation commands", () => {
      const provider = new ModelsTreeProvider();
      const tables: TableInfo[] = [
        {
          name: "cities",
          model: "City",
          loc: { file: "/app/models/city.go", line: 10, col: 0 },
          migLoc: { file: "/database/migrations/cities.go", line: 5, col: 0 },
          columns: [
            {
              name: "id",
              goName: "ID",
              goType: "uint",
              sqlType: "BIGINT",
              doc: "Primary key",
              nullable: false,
              loc: { file: "/app/models/city.go", line: 11, col: 2 },
              migLoc: { file: "", line: 0, col: 0 },
            },
            {
              name: "province_id",
              goName: "ProvinceID",
              goType: "uint",
              sqlType: "",
              doc: "",
              nullable: true,
              foreignKey: "provinces.id",
              loc: { file: "", line: 0, col: 0 },
              migLoc: { file: "/database/migrations/cities.go", line: 8, col: 2 },
            },
          ],
          relations: [
            {
              name: "Province",
              targetModel: "Province",
              targetTable: "provinces",
              foreignKey: "province_id",
              references: "id",
              loc: { file: "/app/models/city.go", line: 15, col: 2 },
            },
            {
              name: "",
              targetModel: "",
              targetTable: "ignored_fk",
              foreignKey: "x",
              references: "id",
              loc: { file: "", line: 0, col: 0 },
            },
          ],
        },
        {
          name: "mig_only",
          model: "",
          loc: { file: "", line: 0, col: 0 },
          migLoc: { file: "/database/migrations/mig_only.go", line: 1, col: 0 },
          columns: [],
        },
      ];

      provider.setData(tables);
      const roots = provider.getChildren();
      assert.equal(roots.length, 2);

      const cityItem = provider.getTreeItem(roots[0]);
      assert.equal(cityItem.label, "cities");
      assert.equal(cityItem.description, "models.City (2 cols)");
      assert.equal(cityItem.contextValue, "goravelTable");
      assert.equal(cityItem.collapsibleState, 1);
      assert.deepEqual(cityItem.command?.arguments, ["/app/models/city.go", 10, 0]);

      const migOnlyItem = provider.getTreeItem(roots[1]);
      assert.equal(migOnlyItem.description, "0 cols");
      assert.equal(migOnlyItem.collapsibleState, 0);
      assert.deepEqual(migOnlyItem.command?.arguments, ["/database/migrations/mig_only.go", 1, 0]);

      const children = provider.getChildren(roots[0]);
      // 2 columns + 1 named relation (unnamed relation filtered out)
      assert.equal(children.length, 3);
      assert.deepEqual(provider.getChildren(children[0]), []);

      const idCol = provider.getTreeItem(children[0]);
      assert.equal(idCol.label, "id");
      assert.equal(idCol.description, "BIGINT NOT NULL");
      assert.equal(idCol.tooltip, "id: Primary key");
      assert.equal((idCol.iconPath as unknown as MockThemeIcon).id, "symbol-field");

      const fkCol = provider.getTreeItem(children[1]);
      assert.equal(fkCol.description, "uint NULL → provinces.id");
      assert.equal(fkCol.tooltip, "province_id (uint)");
      assert.equal((fkCol.iconPath as unknown as MockThemeIcon).id, "key");
      assert.deepEqual(fkCol.command?.arguments, ["/database/migrations/cities.go", 8, 2]);

      const relItem = provider.getTreeItem(children[2]);
      assert.equal(relItem.label, "Province");
      assert.equal(relItem.description, "→ Province (province_id)");
      assert.equal((relItem.iconPath as unknown as MockThemeIcon).id, "references");
    });
  });

  describe("RoutesTreeProvider", () => {
    it("groups routes alphabetically by module and distinguishes HTTP vs gRPC and view routes", () => {
      const provider = new RoutesTreeProvider();
      provider.setData([
        {
          method: "gRPC",
          path: "/UserService/GetUser",
          handler: "UserController.GetUser",
          controller: "UserController",
          action: "GetUser",
          module: "grpc",
          loc: { file: "/routes/grpc.go", line: 4, col: 0 },
          ctrlLoc: { file: "/app/grpc/user.go", line: 12, col: 0 },
        },
        {
          method: "GET",
          path: "/dashboard",
          handler: "PageController.Dashboard",
          controller: "PageController",
          action: "Dashboard",
          module: "api",
          view: "dashboard.tmpl",
          loc: { file: "/routes/api.go", line: 9, col: 0 },
          ctrlLoc: { file: "", line: 0, col: 0 },
        },
        {
          method: "GET",
          path: "/health",
          handler: "closure",
          controller: "",
          action: "",
          module: "",
          loc: { file: "/routes/web.go", line: 2, col: 0 },
          ctrlLoc: { file: "", line: 0, col: 0 },
        },
      ]);

      const modules = provider.getChildren();
      assert.deepEqual(
        modules.map((m) => provider.getTreeItem(m).label),
        ["api", "grpc", "routes"]
      );

      const apiRoutes = provider.getChildren(modules[0]);
      const apiItem = provider.getTreeItem(apiRoutes[0]);
      assert.equal(apiItem.label, "GET /dashboard");
      assert.equal(apiItem.description, "PageController.Dashboard → dashboard.tmpl");
      assert.equal(apiItem.contextValue, "goravelRoute");
      assert.equal((apiItem.iconPath as unknown as MockThemeIcon).id, "globe");
      assert.deepEqual(apiItem.command?.arguments, ["/routes/api.go", 9, 0]);

      const grpcRoutes = provider.getChildren(modules[1]);
      const grpcItem = provider.getTreeItem(grpcRoutes[0]);
      assert.equal((grpcItem.iconPath as unknown as MockThemeIcon).id, "symbol-interface");
      assert.deepEqual(grpcItem.command?.arguments, ["/app/grpc/user.go", 12, 0]);
      assert.deepEqual(provider.getChildren(grpcRoutes[0]), []);
    });
  });

  describe("MigrationsTreeProvider", () => {
    it("groups by folder and reflects applied, pending, and unknown statuses", () => {
      const provider = new MigrationsTreeProvider();
      provider.setData([
        {
          name: "20250101_create_users",
          folder: "migrations",
          table: "users",
          status: "applied",
          batch: 2,
          loc: { file: "/database/migrations/1.go", line: 0, col: 0 },
        },
        {
          name: "20250102_create_posts",
          folder: "migrations",
          table: "posts",
          status: "pending",
          loc: { file: "/database/migrations/2.go", line: 0, col: 0 },
        },
        {
          name: "20250103_audit",
          folder: "",
          table: "",
          loc: { file: "/database/migrations/3.go", line: 0, col: 0 },
        },
      ]);

      const folders = provider.getChildren();
      assert.equal(folders.length, 1);
      const folderItem = provider.getTreeItem(folders[0]);
      assert.equal(folderItem.label, "migrations");
      assert.equal(folderItem.description, "3 files");

      const items = provider.getChildren(folders[0]);
      const applied = provider.getTreeItem(items[0]);
      assert.equal(applied.description, "→ users · Applied (batch 2)");
      assert.equal(applied.contextValue, "goravelMigrationApplied");
      assert.equal((applied.iconPath as unknown as MockThemeIcon).id, "pass");

      const pending = provider.getTreeItem(items[1]);
      assert.equal(pending.description, "→ posts · Pending");
      assert.equal(pending.contextValue, "goravelMigrationPending");
      assert.equal((pending.iconPath as unknown as MockThemeIcon).id, "clock");

      const unknown = provider.getTreeItem(items[2]);
      assert.equal(unknown.description, "");
      assert.equal(unknown.contextValue, "goravelMigration");
      assert.equal((unknown.iconPath as unknown as MockThemeIcon).id, "file-code");
      assert.deepEqual(provider.getChildren(items[2]), []);
    });
  });

  describe("CommandsTreeProvider", () => {
    it("renders default sections when empty and optional sections when populated", () => {
      const provider = new CommandsTreeProvider();
      const defaultSections = provider.getChildren();
      assert.equal(defaultSections.length, 3);
      assert.equal(provider.getTreeItem(defaultSections[0]).collapsibleState, 1); // Collapsed when 0

      provider.setData(
        [{ signature: "app:sync", description: "Sync", structName: "SyncCmd", loc: { file: "/c.go", line: 1, col: 0 } }],
        [{ name: "UserSeeder", signature: "UserSeeder", loc: { file: "/s.go", line: 1, col: 0 } }],
        [{ expr: "*/5 * * * *", human: "Every 5 minutes", target: "app:sync", loc: { file: "/k.go", line: 3, col: 0 } }],
        [{ kind: "migration", name: "m1", file: "/m.go", loc: { file: "/m.go", line: 0, col: 0 } }],
        [{ name: "UserCreated", listeners: ["SendMail"], loc: { file: "/e.go", line: 2, col: 0 } }],
        [{ name: "MailJob", signature: "mail_job", loc: { file: "/j.go", line: 2, col: 0 } }],
        [{ name: "UserObserver", model: "User", hooks: ["Created", "Updated"], loc: { file: "/o.go", line: 2, col: 0 } }]
      );

      const allSections = provider.getChildren();
      assert.equal(allSections.length, 7);
      assert.equal(provider.getTreeItem(allSections[0]).collapsibleState, 2); // Expanded when > 0

      const cmdNode = provider.getChildren(allSections[0])[0];
      const cmdItem = provider.getTreeItem(cmdNode);
      assert.equal(cmdItem.id, "cmd:app:sync");
      assert.equal(cmdItem.command?.command, "goravel.artisan.runCommand");
      assert.deepEqual(cmdItem.command?.arguments, ["app:sync"]);

      const seederItem = provider.getTreeItem(provider.getChildren(allSections[1])[0]);
      assert.deepEqual(seederItem.command?.arguments, ["db:seed --seeder=UserSeeder"]);

      const schedItem = provider.getTreeItem(provider.getChildren(allSections[2])[0]);
      assert.equal(schedItem.label, "app:sync");
      assert.equal(schedItem.tooltip, "*/5 * * * * → Every 5 minutes");

      const eventItem = provider.getTreeItem(provider.getChildren(allSections[3])[0]);
      assert.equal(eventItem.description, "→ SendMail");

      const jobItem = provider.getTreeItem(provider.getChildren(allSections[4])[0]);
      assert.equal(jobItem.label, "mail_job");

      const obsItem = provider.getTreeItem(provider.getChildren(allSections[5])[0]);
      assert.equal(obsItem.description, "→ models.User (Created, Updated)");

      const unregItem = provider.getTreeItem(provider.getChildren(allSections[6])[0]);
      assert.equal(unregItem.contextValue, "goravelUnregistered");
      assert.equal(unregItem.description, "migration (not in bootstrap/)");
      assert.deepEqual(provider.getChildren(cmdNode), []);
    });
  });

  describe("ViewsTreeProvider", () => {
    it("separates app views, package views, shared vars, template funcs, callers, and includes", () => {
      const provider = new ViewsTreeProvider();
      assert.equal(provider.getChildren().length, 1);

      provider.setData(
        [
          {
            name: "pages/home.tmpl",
            relDisplay: "resources/views/pages/home.tmpl",
            source: "app",
            loc: { file: "/views/pages/home.tmpl", line: 0, col: 0 },
            packageLocs: [{ file: "/pkg/views/pages/home.tmpl", line: 0, col: 0 }],
            usedVars: ["title", "user"],
            callers: [
              { viewName: "pages/home.tmpl", method: "Make", controller: "HomeController.Index", route: "GET /", loc: { file: "/ctrl.go", line: 10, col: 0 } },
            ],
            includes: [
              { target: "partials/nav.tmpl", passDot: true, contextExpr: ".", loc: { file: "/views/pages/home.tmpl", line: 2, col: 0 } },
            ],
          },
          {
            name: "partials/nav.tmpl",
            relDisplay: "resources/views/partials/nav.tmpl",
            source: "app",
            loc: { file: "/views/partials/nav.tmpl", line: 0, col: 0 },
            includedBy: ["pages/home.tmpl"],
          },
          {
            name: "empty.tmpl",
            relDisplay: "resources/views/empty.tmpl",
            source: "app",
            loc: { file: "/views/empty.tmpl", line: 0, col: 0 },
            isEmpty: true,
          },
          {
            name: "pkg/dash.tmpl",
            relDisplay: "packages/admin/views/pkg/dash.tmpl",
            source: "package",
            loc: { file: "/pkg/dash.tmpl", line: 0, col: 0 },
          },
        ],
        [{ key: "app_name", valueExpr: `"Goravel"`, controller: "AppProvider", loc: { file: "/p.go", line: 5, col: 0 } }],
        [{ name: "upper", signature: "func(string) string", loc: { file: "/p.go", line: 8, col: 0 } }]
      );

      const sections = provider.getChildren();
      assert.equal(sections.length, 4);
      const sectionItems = sections.map((s) => provider.getTreeItem(s));
      assert.deepEqual(
        sectionItems.map((i) => (i.iconPath as unknown as MockThemeIcon).id),
        ["file-code", "package", "symbol-variable", "symbol-function"]
      );

      const appViewNodes = provider.getChildren(sections[0]);
      assert.equal(appViewNodes.length, 3);

      const homeItem = provider.getTreeItem(appViewNodes[0]);
      assert.equal(homeItem.description, "overrides pkg · 1 caller(s) · 2 vars");
      assert.equal((homeItem.iconPath as unknown as MockThemeIcon).id, "layers");
      assert.match(String(homeItem.tooltip || ""), /Variables: \.title, \.user/);

      const navItem = provider.getTreeItem(appViewNodes[1]);
      assert.equal(navItem.description, "included by 1");

      const emptyItem = provider.getTreeItem(appViewNodes[2]);
      assert.equal(emptyItem.description, "empty · 0 callers");
      assert.equal((emptyItem.iconPath as unknown as MockThemeIcon).id, "warning");

      const homeChildren = provider.getChildren(appViewNodes[0]);
      assert.equal(homeChildren.length, 2);
      const callerItem = provider.getTreeItem(homeChildren[0]);
      assert.equal(callerItem.label, "GET / (HomeController.Index)");
      assert.equal(callerItem.description, "View().Make");

      const incItem = provider.getTreeItem(homeChildren[1]);
      assert.equal(incItem.label, '{{ template "partials/nav.tmpl" }}');
      assert.equal(incItem.description, ".");

      const pkgItem = provider.getTreeItem(provider.getChildren(sections[1])[0]);
      assert.equal(pkgItem.label, "pkg/dash.tmpl");

      const sharedItem = provider.getTreeItem(provider.getChildren(sections[2])[0]);
      assert.equal(sharedItem.label, ".app_name");
      assert.equal(sharedItem.description, `"Goravel" (AppProvider)`);

      const fnItem = provider.getTreeItem(provider.getChildren(sections[3])[0]);
      assert.equal(fnItem.label, "upper");
      assert.equal(fnItem.description, "func(string) string");
      assert.deepEqual(provider.getChildren(homeChildren[0]), []);
    });
  });
});
