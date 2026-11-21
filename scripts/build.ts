// Bun loads the local .env before this script. Environment values override it.
// Pass arguments directly to the child process; OAuth values never become shell
// text or console output. Go's linker parses quotes inside the -X argument.
const providers = ["google", "microsoft"] as const;
const flags: string[] = [];
for (const provider of providers) {
  for (const field of ["ID", "Secret"] as const) {
    const env = `${provider.toUpperCase()}_CLIENT_${field.toUpperCase()}`;
    const value = process.env[env];
    if (value === undefined) continue;
    if (!/^[A-Za-z0-9._~:/+\-=]*$/.test(value)) throw new Error(`Unsupported characters in ${env}`);
    flags.push("-X", `github.com/duanyongcheng/pebble-go/internal/oauth.${provider}Client${field}=${value}`);
  }
}
const child = Bun.spawn(["mygo", "build", ...process.argv.slice(2)], {
  env: { ...process.env, MYGO_LDFLAGS: [process.env.MYGO_LDFLAGS, ...flags].filter(Boolean).join(" ") },
  stdout: "inherit", stderr: "inherit",
});
process.exit(await child.exited);
export {};
