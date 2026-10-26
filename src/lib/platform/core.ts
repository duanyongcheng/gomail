import { commands } from "./manifest";

export function invoke<T>(command: string, args: Record<string, unknown> = {}): Promise<T> {
  if (!Object.prototype.hasOwnProperty.call(commands, command)) return Promise.reject(new Error(`NotImplemented: ${command}`));
  // The upstream invoke API is string-based. The only erased boundary stays
  // here; each implemented command above retains its generated argument type.
  const entry = commands[command as keyof typeof commands];
  const run = entry.run as unknown as (args: Record<string, unknown>) => Promise<T>;
  return run(args);
}

export function convertFileSrc(_path: string): string { return ""; }
