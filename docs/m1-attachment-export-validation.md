# M1 attachment export validation

Validated on 2026-10-27 on top of `9e77ad0` and the existing, uncommitted remote-ops
slice. No real Gmail, keyring, or old Pebble profile was used.

## Automated gate

`bun run check` passed:

| Check | Result |
|---|---|
| CGO-free Go tests | Passed |
| Go vet and internal race tests | Passed |
| MyGO vet and generated bindings | Passed |
| TypeScript | Passed |
| Vitest | 127 files, 732 tests passed |
| Vite production build | Passed |
| CGO-free darwin/amd64, linux/amd64+arm64, windows/amd64+arm64 | Passed |

New tests cover cache lookup/live-message filtering, source containment and
symlink escape, portable filenames, existing-target and symlink-target safety,
concurrent unique destinations, empty files, monotonic progress, cancellation and
read-error cleanup, static diagnostics, native-dialog defaults/cancel/export,
frontend busy/error/cancel/success states, Windows path joining, stale results
on message changes, and typed command routing. The v20 schema oracle still passes.

## Packaged desktop

- `bun run build -skip-dmg -skip-notarize` passed; the existing Vite large-chunk
  warning remains non-fatal.
- `codesign --verify --deep --strict` passed for the macOS arm64 bundle.
- Packaged selftest passed all **nine** checks. The new check opens the message,
  clicks the actual attachment Download button, checks the successful path in
  the DOM, and verifies exported bytes in an isolated Downloads directory.
- Inspected `attachment-export.png`: the footer shows the exported text file,
  successful download check and separate Save As control.
- Normal empty-profile launch stayed alive; database v23 had zero accounts and
  zero pending ops. SIGTERM produced a clean exit (status 0).

Native Save As chooser selection was tested via an injected dialog adapter,
not an automated click through the OS chooser. Windows/Linux runtime validation
and real Gmail verification remain manual; cross-compilation is not a runtime
claim. Only quick-download was exercised end-to-end in the packaged WebView.

## Evidence

- Gate: `/tmp/pebble-go-m1-attachment-export-check.log`.
- Detailed check logs/artifacts:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-check-bP15ky/`.
- Package: `/tmp/pebble-go-m1-attachment-export-package.log`.
- Selftest/normal profiles, report, screenshots and exported file:
  `/var/folders/6m/040v24k96w751shctqx8sw100000gn/T/pebble-go-m1-attachment-export-5vek8wfh/`.
- App: `build/darwin-arm64/Pebble Go.app`.

Evidence is machine-local. Initial assertions compared noncanonical temp paths;
they were corrected to account for macOS `/var` → `/private/var` resolution.
