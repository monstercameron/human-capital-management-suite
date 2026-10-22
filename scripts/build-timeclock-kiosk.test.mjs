import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  chmodSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import path from "node:path";
import test from "node:test";

const repositoryRoot = process.cwd();
const script = path.join(repositoryRoot, "scripts", "build-timeclock-kiosk.sh");
const artifactRoot = path.join(repositoryRoot, ".artifacts");
mkdirSync(artifactRoot, { recursive: true });
const fixtureRoot = mkdtempSync(path.join(artifactRoot, "build-timeclock-kiosk-test-"));

test.after(() => rmSync(fixtureRoot, { recursive: true, force: true }));

const findGitBash = () => {
  if (process.platform !== "win32") return { bash: "bash", cygpath: null };
  const gitResult = spawnSync("where.exe", ["git.exe"], { encoding: "utf8" });
  const gitPaths = (gitResult.stdout ?? "").split(/\r?\n/).filter(Boolean);
  for (const gitPath of gitPaths) {
    const gitRoot = path.resolve(path.dirname(gitPath), "..");
    const bash = path.join(gitRoot, "bin", "bash.exe");
    const cygpath = path.join(gitRoot, "usr", "bin", "cygpath.exe");
    if (existsSync(bash) && existsSync(cygpath)) return { bash, cygpath };
  }
  throw new Error(
    "Git Bash and cygpath.exe are required to test the Bash helper on Windows",
  );
};

const { bash, cygpath } = findGitBash();
const toBashPath = (value) => {
  if (!cygpath) return value;
  const result = spawnSync(cygpath, ["-u", value], {
    encoding: "utf8",
    stdio: "pipe",
  });
  if (result.status !== 0)
    throw new Error(result.stderr || `cygpath failed for ${value}`);
  return result.stdout.trim();
};

const makeGoFixture = (name, options = {}) => {
  const root = path.join(fixtureRoot, name);
  const repository = path.join(root, "repo");
  const scripts = path.join(repository, "scripts");
  const bin = path.join(root, "bin");
  const goroot = path.join(root, "goroot");
  const shim = path.join(goroot, "lib", "wasm", "wasm_exec.js");
  mkdirSync(bin, { recursive: true });
  mkdirSync(scripts, { recursive: true });
  writeFileSync(path.join(scripts, "build-timeclock-kiosk.sh"), readFileSync(script));
  if (options.withShim !== false) {
    mkdirSync(path.dirname(shim), { recursive: true });
    writeFileSync(shim, "// fake toolchain shim\n");
  }
  const fakeGo = path.join(bin, "go");
  writeFileSync(
    fakeGo,
    [
      "#!/usr/bin/env bash",
      "set -eu",
      '[ "$PWD" = "$FAKE_REPO_ROOT" ] || exit 29',
      'if [ "$1" = env ] && [ "$2" = GOROOT ]; then printf "%s\\n" "$FAKE_GOROOT"; exit 0; fi',
      'if [ "$1" != build ]; then exit 30; fi',
      '[ "$GOOS" = js ] && [ "$GOARCH" = wasm ] || exit 31',
      'for name in GOTMPDIR TMP TEMP GOCACHE; do eval "value=\\$$name"; case "$value" in "$FAKE_REPO_ROOT"/.artifacts/*) ;; *) exit 32 ;; esac; done',
      'shift; [ "$1" = -trimpath ] || exit 33; shift; [ "$1" = -o ] || exit 34; shift',
      'output=$1; shift; [ "$1" = ./tools/uxqual/cmd/timeclockwasm ] || exit 35',
      'if [ "${FAKE_GO_FAIL_BUILD:-0}" = 1 ]; then printf partial > "$output"; exit 36; fi',
      'printf "\\000asm\\001\\000\\000\\000" > "$output"; exit 0',
      "",
    ].join("\n"),
  );
  chmodSync(fakeGo, 0o755);
  return { bin, goroot, repository, shim };
};

const runBuild = (fixture, extraEnv = {}, cwd = fixture.repository) =>
  spawnSync(
    bash,
    [toBashPath(path.join(fixture.repository, "scripts", "build-timeclock-kiosk.sh"))],
    {
      cwd,
      encoding: "utf8",
      env: {
        ...process.env,
        PATH: `${fixture.bin}${path.delimiter}${process.env.PATH ?? ""}`,
        FAKE_GOROOT: toBashPath(fixture.goroot),
        FAKE_REPO_ROOT: toBashPath(fixture.repository),
        ...extraEnv,
      },
      stdio: "pipe",
    },
  );

test("helper builds the browser WASM and copies its matching shim under artifacts", () => {
  const fixture = makeGoFixture("complete");
  const result = runBuild(fixture);
  assert.equal(
    result.status,
    0,
    `${result.error?.message ?? ""}${result.stdout ?? ""}${result.stderr ?? ""}`,
  );
  const output = path.join(fixture.repository, ".artifacts", "bin", "timeclock-kiosk");
  const wasm = readFileSync(path.join(output, "timeclock.wasm"));
  const shim = readFileSync(path.join(output, "wasm_exec.js"));
  assert.deepEqual([...wasm.subarray(0, 8)], [0, 97, 115, 109, 1, 0, 0, 0]);
  assert.deepEqual(shim, readFileSync(fixture.shim));
  assert.ok(
    path
      .relative(fixture.repository, output)
      .startsWith(path.join(".artifacts", "bin")),
  );
});

test("helper selects GOROOT and builds from the repository when launched elsewhere", () => {
  const fixture = makeGoFixture("external-working-directory");
  const result = runBuild(fixture, {}, fixtureRoot);
  assert.equal(
    result.status,
    0,
    `${result.error?.message ?? ""}${result.stdout ?? ""}${result.stderr ?? ""}`,
  );
  assert.ok(
    existsSync(
      path.join(
        fixture.repository,
        ".artifacts",
        "bin",
        "timeclock-kiosk",
        "timeclock.wasm",
      ),
    ),
  );
});

test("helper fails before compiling when the matching current-toolchain shim is absent", () => {
  const fixture = makeGoFixture("missing-shim", { withShim: false });
  const result = runBuild(fixture);
  assert.notEqual(result.status, 0, "missing shim unexpectedly built");
  assert.match(result.stderr ?? "", /matching Go shim not found/);
});

test("failed compilation preserves previous assets and removes its temporary bundle", () => {
  const fixture = makeGoFixture("failed-build");
  const output = path.join(fixture.repository, ".artifacts", "bin", "timeclock-kiosk");
  mkdirSync(output, { recursive: true });
  writeFileSync(path.join(output, "timeclock.wasm"), "previous wasm");
  writeFileSync(path.join(output, "wasm_exec.js"), "previous shim");

  const result = runBuild(fixture, { FAKE_GO_FAIL_BUILD: "1" });
  assert.notEqual(result.status, 0, "injected compiler failure unexpectedly succeeded");
  assert.equal(
    readFileSync(path.join(output, "timeclock.wasm"), "utf8"),
    "previous wasm",
  );
  assert.equal(
    readFileSync(path.join(output, "wasm_exec.js"), "utf8"),
    "previous shim",
  );
  assert.deepEqual(readdirSync(output).sort(), ["timeclock.wasm", "wasm_exec.js"]);
});
