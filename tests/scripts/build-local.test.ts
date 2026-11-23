import { spawnSync } from "node:child_process";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

const source = join(process.cwd(), "scripts/build-local.sh");
const fixtures: string[] = [];

type Call = {
  tool: string;
  args: string[];
  cwd: string;
  cgo: string | null;
  goos: string | null;
  goarch: string | null;
};

function fixture() {
  const root = mkdtempSync(join(tmpdir(), "gomail local-build-"));
  fixtures.push(root);
  const bin = join(root, "bin");
  const script = join(root, "scripts/build-local.sh");
  const log = join(root, "commands.jsonl");
  const envMarker = join(root, "env-executed");
  mkdirSync(bin);
  mkdirSync(join(root, "scripts"));
  copyFileSync(source, script);
  symlinkSync("/usr/bin/dirname", join(bin, "dirname"));
  writeFileSync(join(root, ".env"), `printf executed > "${envMarker}"\n`);

  for (const tool of ["uname", "go", "bun", "node", "codesign"]) {
    writeFileSync(join(bin, tool), `#!${process.execPath}
const fs = require("node:fs");
const tool = ${JSON.stringify(tool)};
const args = process.argv.slice(2);
fs.appendFileSync(process.env.COMMAND_LOG, JSON.stringify({
  tool, args, cwd: process.cwd(), cgo: process.env.CGO_ENABLED ?? null,
  goos: process.env.GOOS ?? null, goarch: process.env.GOARCH ?? null,
}) + "\\n");
if (process.env.FAIL_TOOL === tool && process.env.FAIL_ARG === args[0]) process.exit(23);
if (tool === "uname") console.log(process.env.MOCK_OS ?? "Darwin");
if (tool === "go") console.log(process.env.MOCK_ARCH ?? "arm64");
`, { mode: 0o755 });
  }

  function run(extraEnv: Record<string, string> = {}) {
    const result = spawnSync("/bin/bash", [script], {
      cwd: tmpdir(),
      env: {
        ...process.env, PATH: bin, COMMAND_LOG: log,
        GOOS: "windows", GOARCH: "386", CGO_ENABLED: "1", ...extraEnv,
      },
      encoding: "utf8",
    });
    if (result.error) throw result.error;
    const calls: Call[] = existsSync(log)
      ? readFileSync(log, "utf8").trim().split("\n").map((line) => JSON.parse(line))
      : [];
    return { ...result, calls };
  }
  return { root, bin, envMarker, run };
}

afterEach(() => {
  for (const root of fixtures.splice(0)) rmSync(root, { recursive: true, force: true });
});

describe("local macOS build script", () => {
  it.each(["arm64", "amd64"])("builds the %s host from an unrelated cwd with spaces in the project path", (arch) => {
    const { root, envMarker, run } = fixture();
    const { status, stdout, calls } = run({ MOCK_ARCH: arch });
    const app = join(root, `build/darwin-${arch}/GoMail.app`);
    expect(status).toBe(0);
    expect(calls.map(({ tool, args }) => [tool, ...args])).toEqual([
      ["uname", "-s"],
      ["go", "env", "GOHOSTARCH"],
      ["bun", "install", "--frozen-lockfile"],
      ["bun", "run", "build", "-platform", `darwin/${arch}`, "-o", "build", "-skip-dmg", "-skip-notarize", "-sign", "-"],
      ["codesign", "--verify", "--deep", "--strict", app],
    ]);
    for (const call of calls.slice(1)) {
      expect(call.cwd).toBe(realpathSync(root));
      expect(call.cgo).toBe("0");
      expect(call.goos).toBeNull();
      expect(call.goarch).toBeNull();
    }
    expect(stdout).toContain(`Local build ready: ${app}`);
    expect(stdout).toContain(`open "${app}"`);
    expect(existsSync(envMarker)).toBe(false);
  });

  it.each([
    ["bun", "install", 3],
    ["bun", "run", 4],
    ["codesign", "--verify", 5],
  ] as const)("propagates %s %s failure and does not continue", (tool, arg, count) => {
    const { run } = fixture();
    const { status, stdout, calls } = run({ FAIL_TOOL: tool, FAIL_ARG: arg });
    expect(status).toBe(23);
    expect(calls).toHaveLength(count);
    expect(stdout).not.toContain("Local build ready:");
  });

  it("rejects non-macOS hosts before dependency installation", () => {
    const { run } = fixture();
    const { status, stderr, calls } = run({ MOCK_OS: "Linux" });
    expect(status).toBe(1);
    expect(stderr).toContain("requires macOS");
    expect(calls.map(({ tool }) => tool)).toEqual(["uname"]);
  });

  it.each(["go", "bun", "node", "codesign"])("reports missing %s before running build steps", (tool) => {
    const { bin, run } = fixture();
    rmSync(join(bin, tool));
    const { status, stderr, calls } = run();
    expect(status).toBe(1);
    expect(stderr).toContain(`Missing required tool: ${tool}`);
    expect(calls.map(({ tool: name }) => name)).toEqual(["uname"]);
  });
});
