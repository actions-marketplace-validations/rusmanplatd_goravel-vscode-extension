import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, "..");
const actionPath = path.join(repoRoot, "action.yml");
const actionYaml = fs.readFileSync(actionPath, "utf8");

/**
 * Extracts `run: |` script blocks from action.yml by step name so tests always
 * exercise the exact shell logic declared in action.yml.
 */
function extractStepRunScript(yamlText, stepName) {
  const lines = yamlText.split("\n");
  let inStep = false;
  let inRun = false;
  let runIndent = -1;
  const collected = [];

  for (const line of lines) {
    if (!inStep) {
      if (line.trim() === `- name: ${stepName}`) {
        inStep = true;
      }
      continue;
    }
    if (!inRun) {
      if (/^\s*-\s+name:/.test(line)) break;
      if (line.trim() === "run: |") {
        inRun = true;
      }
      continue;
    }
    if (line.trim() === "") {
      collected.push("");
      continue;
    }
    const indent = line.search(/\S/);
    if (runIndent < 0) runIndent = indent;
    if (indent < runIndent) break;
    collected.push(line.slice(runIndent));
  }
  assert.ok(collected.length > 0, `Could not extract run block for step "${stepName}"`);
  return collected.join("\n") + "\n";
}

function extractInputDefault(yamlText, inputName) {
  const re = new RegExp(`^  ${inputName}:\\n(?:    .*\\n)*?    default:\\s*"([^"]*)"`, "m");
  const m = yamlText.match(re);
  return m ? m[1] : null;
}

describe("action.yml", () => {
  const installStepScript = extractStepRunScript(actionYaml, "Install goravel-lsp");
  const runStepScript = extractStepRunScript(actionYaml, "Run goravel-lsp");

  it("declares valid composite action metadata, inputs, defaults, and working-directory", () => {
    assert.match(actionYaml, /^name:\s*"goravel-lsp"/m);
    assert.match(actionYaml, /using:\s*"composite"/);
    assert.equal(extractInputDefault(actionYaml, "version"), "latest");
    assert.equal(extractInputDefault(actionYaml, "working-directory"), ".");
    assert.equal(extractInputDefault(actionYaml, "args"), "--check-sql");
    assert.equal(extractInputDefault(actionYaml, "only-sql"), "true");
    assert.equal(extractInputDefault(actionYaml, "only-templates"), "false");
    assert.match(actionYaml, /working-directory:\s*\$\{\{\s*inputs\.working-directory\s*\}\}/);
    assert.match(actionYaml, /shell:\s*bash/);
  });

  it("handles install step version routing (local build vs install.sh) and writes GITHUB_PATH", () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-act-inst-"));
    try {
      const runnerTemp = path.join(tmp, "runner-temp");
      const ghPathFile = path.join(tmp, "github-path");
      const actionDir = path.join(tmp, "action");
      const mockBin = path.join(tmp, "bin");
      fs.mkdirSync(runnerTemp, { recursive: true });
      fs.mkdirSync(path.join(actionDir, "server"), { recursive: true });
      fs.mkdirSync(mockBin, { recursive: true });
      fs.writeFileSync(ghPathFile, "");

      // Mock install.sh inside actionDir
      fs.writeFileSync(
        path.join(actionDir, "install.sh"),
        `#!/bin/sh
dir=""
ver=""
while [ $# -gt 0 ]; do
  case "$1" in
    -b) dir="$2"; shift 2 ;;
    *) ver="$1"; shift ;;
  esac
done
mkdir -p "$dir"
printf "#!/bin/sh\\necho installed-%s\\n" "$ver" > "$dir/goravel-lsp"
chmod +x "$dir/goravel-lsp"
`,
        { mode: 0o755 }
      );

      // Mock go binary
      fs.writeFileSync(
        path.join(mockBin, "go"),
        `#!/bin/sh
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf "#!/bin/sh\\necho built-local\\n" > "$out"
chmod +x "$out"
`,
        { mode: 0o755 }
      );

      // Case 1: version=latest with server/ present -> builds locally
      const resLocal = spawnSync("bash", ["-c", installStepScript], {
        encoding: "utf8",
        env: {
          ...process.env,
          PATH: `${mockBin}:${process.env.PATH}`,
          RUNNER_TEMP: runnerTemp,
          GITHUB_PATH: ghPathFile,
          GITHUB_ACTION_PATH: actionDir,
          INPUT_VERSION: "latest",
        },
      });
      assert.equal(resLocal.status, 0, resLocal.stderr);
      assert.match(resLocal.stdout, /built-local/);
      assert.match(fs.readFileSync(ghPathFile, "utf8"), /goravel-lsp-bin/);

      // Case 2: pinned version tag -> delegates to install.sh
      const resTag = spawnSync("bash", ["-c", installStepScript], {
        encoding: "utf8",
        env: {
          ...process.env,
          PATH: `${mockBin}:${process.env.PATH}`,
          RUNNER_TEMP: runnerTemp,
          GITHUB_PATH: ghPathFile,
          GITHUB_ACTION_PATH: actionDir,
          INPUT_VERSION: "v0.9.2",
        },
      });
      assert.equal(resTag.status, 0, resTag.stderr);
      assert.match(resTag.stdout, /installed-v0\.9\.2/);
    } finally {
      fs.rmSync(tmp, { recursive: true, force: true });
    }
  });

  it("resolves all combinations of args, only-sql, and only-templates accurately", () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-act-run-"));
    try {
      const mockBin = path.join(tmp, "bin");
      fs.mkdirSync(mockBin, { recursive: true });
      fs.writeFileSync(
        path.join(mockBin, "goravel-lsp"),
        `#!/bin/sh\nprintf "%s\\n" "$*"\n`,
        { mode: 0o755 }
      );

      const cases = [
        {
          name: "default inputs",
          args: "--check-sql",
          onlySql: "true",
          onlyTemplates: "false",
          want: "--check-sql --out-format=github-actions --only-sql .",
        },
        {
          name: "only-templates=true replaces default --check-sql",
          args: "--check-sql",
          onlySql: "true",
          onlyTemplates: "true",
          want: "--check-templates --out-format=github-actions .",
        },
        {
          name: "only-templates=true with custom args appends --only-templates",
          args: "check --errors-only",
          onlySql: "true",
          onlyTemplates: "true",
          want: "check --errors-only --out-format=github-actions --only-templates .",
        },
        {
          name: "args=--check-templates does not force --only-sql",
          args: "--check-templates",
          onlySql: "true",
          onlyTemplates: "false",
          want: "--check-templates --out-format=github-actions .",
        },
        {
          name: "args=--check-sql --check-templates keeps both SQL and templates",
          args: "--check-sql --check-templates",
          onlySql: "true",
          onlyTemplates: "false",
          want: "--check-sql --check-templates --out-format=github-actions --only-sql .",
        },
        {
          name: "regression: args=--check --all is not overridden by default only-sql=true",
          args: "--check --all",
          onlySql: "true",
          onlyTemplates: "false",
          want: "--check --all --out-format=github-actions .",
        },
        {
          name: "only-sql=false with --check omits --only-sql",
          args: "--check",
          onlySql: "false",
          onlyTemplates: "false",
          want: "--check --out-format=github-actions .",
        },
      ];

      for (const tc of cases) {
        const res = spawnSync("bash", ["-c", runStepScript], {
          encoding: "utf8",
          cwd: tmp,
          env: {
            ...process.env,
            PATH: `${mockBin}:${process.env.PATH}`,
            INPUT_ARGS: tc.args,
            INPUT_ONLY_SQL: tc.onlySql,
            INPUT_ONLY_TEMPLATES: tc.onlyTemplates,
          },
        });
        assert.equal(res.status, 0, `${tc.name} failed: ${res.stderr}`);
        assert.equal(res.stdout.trim(), tc.want, tc.name);
      }
    } finally {
      fs.rmSync(tmp, { recursive: true, force: true });
    }
  });

  it("propagates failures and emits GitHub Actions annotations when run against real goravel-lsp in working-directory", () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-act-e2e-"));
    try {
      const binDir = path.join(tmp, "bin");
      const subProject = path.join(tmp, "services", "api");
      fs.mkdirSync(binDir, { recursive: true });
      fs.mkdirSync(path.join(subProject, "app", "models"), { recursive: true });
      fs.mkdirSync(path.join(subProject, "app", "repositories"), { recursive: true });

      const buildRes = spawnSync("go", ["build", "-o", path.join(binDir, "goravel-lsp"), "."], {
        cwd: path.join(repoRoot, "server"),
        encoding: "utf8",
      });
      assert.equal(buildRes.status, 0, buildRes.stderr);

      fs.writeFileSync(
        path.join(subProject, "app", "models", "user.go"),
        `package models\ntype User struct { ID uint }\nfunc (User) TableName() string { return "users" }\n`
      );
      fs.writeFileSync(
        path.join(subProject, "app", "repositories", "user_repo.go"),
        `package repositories\nimport "github.com/goravel/framework/facades"\nfunc F() { _ = facades.DB().Table("users").Where("missing_col = ?", 1).Get(nil) }\n`
      );

      const res = spawnSync("bash", ["-c", runStepScript], {
        encoding: "utf8",
        cwd: subProject,
        env: {
          ...process.env,
          PATH: `${binDir}:${process.env.PATH}`,
          INPUT_ARGS: "--check-sql",
          INPUT_ONLY_SQL: "true",
          INPUT_ONLY_TEMPLATES: "false",
        },
      });

      assert.equal(res.status, 1, "failing check must propagate exit code 1");
      assert.match(res.stdout, /::error file=app\/repositories\/user_repo\.go,line=\d+,col=\d+::Column 'missing_col' does not exist/);
    } finally {
      fs.rmSync(tmp, { recursive: true, force: true });
    }
  });
});
