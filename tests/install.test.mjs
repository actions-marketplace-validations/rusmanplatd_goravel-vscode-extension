import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, "..");
const installScript = path.join(repoRoot, "install.sh");

function whichSystem(cmd) {
  const res = spawnSync("sh", ["-c", `command -v "${cmd}"`], { encoding: "utf8" });
  return res.status === 0 ? res.stdout.trim() : "";
}

/**
 * Builds an isolated PATH directory containing only selected system utilities
 * plus custom executable mock scripts. Guarantees zero real network access.
 */
function createMockEnv(tmpDir, mocks = {}, { includeSystem = [] } = {}) {
  const binDir = path.join(tmpDir, "mock-bin");
  fs.mkdirSync(binDir, { recursive: true });

  const baseUtils = [
    "sh",
    "tr",
    "sed",
    "awk",
    "grep",
    "head",
    "mktemp",
    "mkdir",
    "rm",
    "install",
    "tar",
    "gzip",
    "basename",
    "dirname",
    "cat",
    "chmod",
    "cp",
    "mv",
    "printf",
    ...includeSystem,
  ];
  for (const util of baseUtils) {
    if (Object.prototype.hasOwnProperty.call(mocks, util)) continue;
    const p = whichSystem(util);
    if (p) {
      const dst = path.join(binDir, util);
      if (!fs.existsSync(dst)) {
        fs.symlinkSync(p, dst);
      }
    }
  }

  for (const [name, body] of Object.entries(mocks)) {
    const p = path.join(binDir, name);
    fs.writeFileSync(p, `#!/bin/sh\n${body}\n`, { mode: 0o755 });
  }

  return binDir;
}

function makeTarGzArchive(tmpDir, archiveName, binaryName, binaryContent) {
  const stage = path.join(tmpDir, "stage-" + archiveName);
  fs.mkdirSync(stage, { recursive: true });
  fs.writeFileSync(path.join(stage, binaryName), binaryContent, { mode: 0o755 });
  fs.writeFileSync(path.join(stage, "LICENSE"), "MIT\n");
  const outPath = path.join(tmpDir, archiveName);
  const res = spawnSync("tar", ["-czf", outPath, "-C", stage, binaryName, "LICENSE"]);
  assert.equal(res.status, 0, `tar failed: ${res.stderr?.toString()}`);
  const sha = crypto.createHash("sha256").update(fs.readFileSync(outPath)).digest("hex");
  return { outPath, sha };
}

function runInstall(args, binDir, extraEnv = {}) {
  return spawnSync("sh", [installScript, ...args], {
    encoding: "utf8",
    env: {
      PATH: binDir,
      HOME: os.tmpdir(),
      ...extraEnv,
    },
  });
}

describe("install.sh", () => {
  it("prints usage and exits 2 on -h", () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-inst-"));
    try {
      const binDir = createMockEnv(tmp, {}, { includeSystem: ["uname"] });
      const res = runInstall(["-h"], binDir);
      assert.equal(res.status, 2);
      assert.match(res.stdout + res.stderr, /Usage:/);
    } finally {
      fs.rmSync(tmp, { recursive: true, force: true });
    }
  });

  it("detects linux/amd64, resolves latest tag via curl, verifies sha256sum, extracts tar.gz, and sets 0755 permissions", () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-inst-"));
    try {
      const installDir = path.join(tmp, "out", "bin");
      const { outPath, sha } = makeTarGzArchive(tmp, "goravel-lsp-0.9.2-linux-amd64.tar.gz", "goravel-lsp", "#!/bin/sh\necho ok\n");
      const checksums = path.join(tmp, "checksums.txt");
      fs.writeFileSync(checksums, `${sha}  goravel-lsp-0.9.2-linux-amd64.tar.gz\n`);

      const binDir = createMockEnv(
        tmp,
        {
          uname: `if [ "$1" = "-s" ]; then echo Linux; else echo x86_64; fi`,
          curl: `
out=""
url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
case "$url" in
  */releases/latest) printf '{"tag_name": "v0.9.2"}\\n' ;;
  *goravel-lsp-0.9.2-linux-amd64.tar.gz) cp "${outPath}" "$out" ;;
  *checksums.txt) cp "${checksums}" "$out" ;;
  *) exit 22 ;;
esac
`,
        },
        { includeSystem: ["sha256sum"] }
      );

      const res = runInstall(["-b", installDir, "-d"], binDir);
      assert.equal(res.status, 0, `stderr:\n${res.stderr}`);
      assert.match(res.stderr, /verified sha256 checksum/);
      assert.match(res.stderr, /installed .*goravel-lsp \(v0\.9\.2\)/);

      const installedBin = path.join(installDir, "goravel-lsp");
      assert.ok(fs.existsSync(installedBin));
      const mode = fs.statSync(installedBin).mode & 0o777;
      assert.equal(mode, 0o755);
    } finally {
      fs.rmSync(tmp, { recursive: true, force: true });
    }
  });

  it("supports darwin/arm64 via wget and shasum -a 256 fallback when curl and sha256sum are absent", () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-inst-"));
    try {
      const installDir = path.join(tmp, "bin");
      const { outPath, sha } = makeTarGzArchive(tmp, "goravel-lsp-1.2.3-darwin-arm64.tar.gz", "goravel-lsp", "#!/bin/sh\necho darwin\n");
      const checksums = path.join(tmp, "checksums.txt");
      fs.writeFileSync(checksums, `${sha}  goravel-lsp-1.2.3-darwin-arm64.tar.gz\n`);

      const binDir = createMockEnv(tmp, {
        uname: `if [ "$1" = "-s" ]; then echo Darwin; else echo arm64; fi`,
        shasum: `echo "${sha}  $3"`,
        wget: `
out=""
url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -qO-) url="$2"; shift 2; echo '{"tag_name": "v1.2.3"}'; exit 0 ;;
    -O) out="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
case "$url" in
  *goravel-lsp-1.2.3-darwin-arm64.tar.gz) cp "${outPath}" "$out" ;;
  *checksums.txt) cp "${checksums}" "$out" ;;
  *) exit 8 ;;
esac
`,
      });

      const res = runInstall(["-b", installDir, "v1.2.3"], binDir);
      assert.equal(res.status, 0, `stderr:\n${res.stderr}`);
      assert.match(res.stderr, /verified sha256 checksum/);
      assert.ok(fs.existsSync(path.join(installDir, "goravel-lsp")));
    } finally {
      fs.rmSync(tmp, { recursive: true, force: true });
    }
  });

  it("handles Windows (MINGW/MSYS) zip extraction and .exe binary suffix", () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-inst-"));
    try {
      const installDir = path.join(tmp, "winbin");
      const fakeZip = path.join(tmp, "win.zip");
      fs.writeFileSync(fakeZip, "PK\x03\x04fakezip");

      const binDir = createMockEnv(tmp, {
        uname: `if [ "$1" = "-s" ]; then echo MINGW64_NT-10.0; else echo x86_64; fi`,
        curl: `
out=""
url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
case "$url" in
  *goravel-lsp-0.9.2-windows-amd64.zip) cp "${fakeZip}" "$out" ;;
  *) exit 22 ;;
esac
`,
        unzip: `
dest=""
while [ $# -gt 0 ]; do
  case "$1" in
    -d) dest="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf "PE_EXE" > "$dest/goravel-lsp.exe"
`,
      });

      const res = runInstall(["-b", installDir, "v0.9.2"], binDir);
      assert.equal(res.status, 0, `stderr:\n${res.stderr}`);
      assert.ok(fs.existsSync(path.join(installDir, "goravel-lsp.exe")));
    } finally {
      fs.rmSync(tmp, { recursive: true, force: true });
    }
  });

  it("aborts installation on sha256 checksum mismatch and does not install binary", () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-inst-"));
    try {
      const installDir = path.join(tmp, "bin");
      const { outPath } = makeTarGzArchive(tmp, "goravel-lsp-0.9.2-linux-amd64.tar.gz", "goravel-lsp", "tampered");
      const checksums = path.join(tmp, "checksums.txt");
      fs.writeFileSync(checksums, `0000000000000000000000000000000000000000000000000000000000000000  goravel-lsp-0.9.2-linux-amd64.tar.gz\n`);

      const binDir = createMockEnv(
        tmp,
        {
          uname: `if [ "$1" = "-s" ]; then echo Linux; else echo x86_64; fi`,
          curl: `
out=""
url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
case "$url" in
  *tar.gz) cp "${outPath}" "$out" ;;
  *checksums.txt) cp "${checksums}" "$out" ;;
  *) exit 22 ;;
esac
`,
        },
        { includeSystem: ["sha256sum"] }
      );

      const res = runInstall(["-b", installDir, "v0.9.2"], binDir);
      assert.notEqual(res.status, 0);
      assert.match(res.stderr, /checksum mismatch/);
      assert.equal(fs.existsSync(path.join(installDir, "goravel-lsp")), false);
    } finally {
      fs.rmSync(tmp, { recursive: true, force: true });
    }
  });

  it("fails on corrupted tar.gz archive without installing binary", () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-inst-"));
    try {
      const installDir = path.join(tmp, "bin");
      const corrupt = path.join(tmp, "corrupt.tar.gz");
      fs.writeFileSync(corrupt, "not a gzip file");

      const binDir = createMockEnv(tmp, {
        uname: `if [ "$1" = "-s" ]; then echo Linux; else echo x86_64; fi`,
        curl: `
out=""
url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
case "$url" in
  *tar.gz) cp "${corrupt}" "$out" ;;
  *) exit 22 ;;
esac
`,
      });

      const res = runInstall(["-b", installDir, "v0.9.2"], binDir);
      assert.notEqual(res.status, 0);
      assert.equal(fs.existsSync(path.join(installDir, "goravel-lsp")), false);
    } finally {
      fs.rmSync(tmp, { recursive: true, force: true });
    }
  });

  it("falls back to build_from_source on unsupported platform/missing asset and cleans up temp directories", () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-inst-"));
    const customTmp = path.join(tmp, "worktmp");
    fs.mkdirSync(customTmp, { recursive: true });
    try {
      const installDir = path.join(tmp, "bin");
      const binDir = createMockEnv(tmp, {
        uname: `if [ "$1" = "-s" ]; then echo FreeBSD; else echo i686; fi`,
        curl: `exit 22`,
        git: `
dest=""
for a in "$@"; do dest="$a"; done
mkdir -p "$dest/server"
`,
        go: `
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf "#!/bin/sh\\necho built-from-source\\n" > "$out"
`,
      });

      const res = runInstall(["-b", installDir, "v0.9.2"], binDir, { TMPDIR: customTmp });
      assert.equal(res.status, 0, `stderr:\n${res.stderr}`);
      assert.match(res.stderr, /freebsd\/386/);
      assert.match(res.stderr, /falling back to source build/);
      assert.ok(fs.existsSync(path.join(installDir, "goravel-lsp")));
      // Regression check for Bug #6: neither first nor second mktemp directory should be leaked
      assert.deepEqual(fs.readdirSync(customTmp), []);
    } finally {
      fs.rmSync(tmp, { recursive: true, force: true });
    }
  });

  it("supports build_from_source without git via source tarball download and errors clearly when go or curl/wget are missing", () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-inst-"));
    try {
      // 1. Source build with main tag and no go toolchain -> fails with actionable error
      const noGoBin = createMockEnv(tmp, {
        uname: `if [ "$1" = "-s" ]; then echo Linux; else echo x86_64; fi`,
      });
      const resNoGo = runInstall(["-b", path.join(tmp, "b1"), "main"], noGoBin);
      assert.notEqual(resNoGo.status, 0);
      assert.match(resNoGo.stderr, /go toolchain is required/);

      // 2. Source build with go, no git, and neither curl nor wget -> fails clearly
      const noHttpTmp = fs.mkdtempSync(path.join(os.tmpdir(), "goravel-nohttp-"));
      try {
        const noHttpBin = createMockEnv(noHttpTmp, {
          uname: `if [ "$1" = "-s" ]; then echo Linux; else echo x86_64; fi`,
          go: `exit 0`,
        });
        const resNoHttp = runInstall(["-b", path.join(noHttpTmp, "b2"), "source"], noHttpBin);
        assert.notEqual(resNoHttp.status, 0);
        assert.match(resNoHttp.stderr, /neither curl nor wget found/);
      } finally {
        fs.rmSync(noHttpTmp, { recursive: true, force: true });
      }
    } finally {
      fs.rmSync(tmp, { recursive: true, force: true });
    }
  });
});
