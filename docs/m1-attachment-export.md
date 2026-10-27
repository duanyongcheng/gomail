# M1 attachment download and Save As

Cached attachments can now be exported from the message footer:

- **Download** saves to the platform Downloads directory (including Linux XDG
  user-directory preferences).
- **Save As** opens the native MyGO save dialog with a sanitized suggested name.
  Cancel returns `null` without writing a file or showing an error.
- Existing targets are never overwritten, even after a dialog's overwrite
  confirmation. A collision chooses `report (1).txt`, then `(2)`, etc., with at
  most 1,000 candidates. The successful button tooltip shows the actual path.

The controls show progress, disable further saves in that attachment list while
busy, and surface failures as a toast. Switching messages resets the view state
and ignores old asynchronous results. A started export can finish after switching
messages; closing the page/window or application cancels its copy.

## Layers and contracts

```text
AttachmentList
  -> downloadDir + api.downloadAttachment / saveAttachmentAs
  -> generated AttachmentService / desktop.FileService
  -> store.Attachment + store.OpenAttachment
  -> constrained cache file -> exclusive destination -> progress event
```

The original `api.ts` signatures remain compatible:

- `get_attachment_path({attachmentId}) -> string | null`
- `download_attachment({attachmentId, saveTo}) -> string`

`FileService.downloads({})` and
`FileService.saveAttachment({attachmentId}) -> string | null` are typed desktop
bindings, not extra invoke commands. The command inventory remains 145, with 38
implemented entries. Core packages do not import MyGO; only desktop owns paths
and native dialogs. No new migration is required; schema remains v23.

## File safety

The source must belong to a live message. Its path must be relative under
`attachments/`; the account/message relationship is resolved from storage, not
from a caller-supplied filesystem path. `store.OpenAttachment` uses `os.Root`
to constrain symlink resolution to the attachment directory, checks for a regular
file and verifies the cached size. CID protocol reads use the same safe opener.

Export validates an absolute, clean destination path and a portable filename.
A pinned destination directory and exclusive creation prevent truncation of an
existing file or a symlink target. The copy uses a 64 KiB buffer and private file
permissions, checks cancellation between chunks, flushes and closes before
reporting completion, and removes its output on copy/flush/close/cancel failure.
Errors shown to the frontend are static and do not include raw filesystem or
mail content diagnostics. macOS paths may be returned in canonical form
(`/private/var/...` rather than `/var/...`).

## Boundaries

This exports existing cache bytes only: it does not fetch from Gmail, automatically
open files, execute attachments or add attachment previews. A missing file is
reported as unavailable; an explicit account sync may recover it when the remote
message is fetched again. Sending and compose attachment staging are separate
M1 work. Interrupted copies from a hard process crash may leave a partial export;
there is no persistent export job/restart cleanup in this slice.

See [validation](m1-attachment-export-validation.md).
