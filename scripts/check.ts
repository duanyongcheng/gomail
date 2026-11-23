import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

const artifacts = await mkdtemp(join(tmpdir(), "gomail-check-"));
const keepArtifacts = process.env.GOMAIL_CHECK_KEEP_ARTIFACTS === "1";

async function run(name: string, command: string[], extraEnv: Record<string, string> = {}) {
  const child = Bun.spawn(command, { env: { ...process.env, GOTOOLCHAIN: "go1.27.3", ...extraEnv }, stdout: "pipe", stderr: "pipe" });
  const [code, stdout, stderr] = await Promise.all([child.exited, new Response(child.stdout).text(), new Response(child.stderr).text()]);
  await Bun.write(join(artifacts, `${name}.log`), stdout + stderr);
  if (code !== 0) {
    console.error(`${name} failed\n${stdout}${stderr}`);
    console.error(`Check logs and compile artifacts kept at: ${artifacts}`);
    process.exit(code);
  }
  console.log(`${name}: passed`);
}

console.log(`Check logs and compile artifacts: ${artifacts}`);
await run("go-test", ["go", "test", "-count=1", "./..."], { CGO_ENABLED: "0" });
await run("go-vet", ["go", "vet", "./..."], { CGO_ENABLED: "0" });
await run("go-race", ["go", "test", "-race", "-count=1", "./internal/..."], { CGO_ENABLED: "1" });
await run("mygo-vet", ["go", "tool", "mygo", "vet", "."]);
await run("bindings", ["go", "tool", "mygo", "generate"]);
await run("typescript", ["bun", "run", "typecheck"]);
await run("vitest", ["bun", "run", "test"]);
await run("vite", ["bun", "run", "build:web"]);
for (const [os, arch] of [["darwin", "amd64"], ["linux", "amd64"], ["linux", "arm64"], ["windows", "amd64"], ["windows", "arm64"]]) {
  const name = `${os}-${arch}`;
  await run(name, ["go", "build", "-o", join(artifacts, name + (os === "windows" ? ".exe" : "")), "."], { GOOS: os, GOARCH: arch, CGO_ENABLED: "0" });
}

if (keepArtifacts) {
  console.log(`Kept check artifacts at: ${artifacts}`);
} else {
  await rm(artifacts, { recursive: true, force: true });
  console.log("Cleaned up temporary check artifacts.");
}

export {};
