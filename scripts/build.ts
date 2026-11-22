// OAuth credentials are configured inside the app (or via runtime environment
// variables during local development) and are never embedded when packaging.
const child = Bun.spawn(["mygo", "build", ...process.argv.slice(2)], {
  env: process.env,
  stdout: "inherit",
  stderr: "inherit",
});
process.exit(await child.exited);
export {};
