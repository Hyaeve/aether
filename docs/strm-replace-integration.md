# STRM content replacement integration

This plugin uses bounded in-memory job state. Its protected routes are registered in `App.Handler`, the component is available in ToolsPage, and App polls recent jobs for the activity popover.

## Backend

Inside `App.Handler`, after creating the `http.ServeMux`, add:

```go
a.RegisterStrmReplace(mux)
```

This registers administrator-session-protected `GET` and `POST /api/strm-replace`. Do not expose `strmReplace` without the protected wrapper. Independent DAV accounts do not have administrator sessions.

POST body:

```json
{"directory":"/media/strm","find":"http://old:15151","replace":"https://new.example","confirmed":true}
```

POST returns HTTP 202 and a task immediately; filesystem scanning runs in the background. Invalid input is 400, an existing active job is 409. Nonexistent or inaccessible directories become failed background tasks. Directory paths must be absolute. `find` must be nonempty; an empty `replace` deletes matching content. Matching is literal and case-sensitive, not regex. Only the extension is case-insensitive.

GET returns `{"tasks":[...]}` newest first, retaining at most 16 tasks per App in memory. GET with `?id=...` returns one task or 404 when unknown/evicted. Both are no-store. Task fields:

- `id`, `directory`, `status`: running / completed / failed.
- `scanned`: all visited filesystem entries, including directories and skipped entries.
- `processed`: eligible STRM files successfully examined; `changed`: successfully replaced files.
- `skipped`: symlinks, special files, and oversized STRM files. Ordinary non-STRM files are ignored.
- `error`, `time`, `updatedAt`, optional `finishedAt`: UTC RFC3339 timestamps.

For global activity notifications, poll GET every 1.5-3 seconds while authenticated, without overlapping requests. Key activity by `id`; show counters, not a fabricated percentage. Notify once when an observed running task reaches a terminal status. Stop polling/clear timers on logout and discard responses from older sessions. No find/replace text or STRM contents are included in task state. Restart clears history and does not resume jobs.

## Frontend

Import `web/src/components/StrmReplace.vue` and conditionally mount it from the desired tool entry:

```vue
<StrmReplace v-if="showStrmReplace" @close="showStrmReplace = false"
  @started="onReplacementStarted" @progress="onReplacementProgress" />
```

No props are required. `started` emits the accepted task; `progress` emits the newest task or null. The component uses the existing API helper, Modal and LocalDirectoryPicker. It polls while mounted; closing it stops UI polling, not the background task. Global notification polling must therefore be owned by the parent application. The second modal captures a snapshot of the submitted fields and only its confirmation sends POST.

## Safety and limits

- Maximum 10,000 scanned entries, 32 subdirectory levels, 1 MiB input/output per file, 16 KiB per find/replace field, 128 KiB JSON request, 30-minute cooperative deadline.
- Symbolic link entries and symlink directory components are rejected/skipped. Each subdirectory is opened relative to a pinned `os.Root`; names and BOM/newlines remain untouched.
- Replacement is staged in the same directory, synced, permission bits preserved, then renamed. Identity, mode, size, modification time and original contents are checked before publishing; detected external changes stop the job without overwriting that file. Temporary files are removed on normal failure.
- Portable filesystems do not provide compare-and-swap rename: a final check-to-rename race remains with uncooperative concurrent writers. Run against a quiescent directory for this guarantee; do not advertise fully atomic conflict detection. Windows and unusual/network filesystems may also have weaker rename guarantees than local Unix filesystems.
- There is no transaction across the directory: earlier successful replacements remain if a later file fails. No undo, persistent history, ACL/ownership preservation, or crash-recovery journal is provided. Hard-linked originals are not modified in place. No user config/data directories or services are modified by development tests.

## Focused verification

```text
go test ./internal/app -run TestStrmReplace -count=1 -v
cd web
node tests/strm-replace-component.mjs
```

The standalone component test starts and closes its own ephemeral Vite fixture, mocks every API request, exercises desktop/mobile confirmation and progress, and places screenshots in the OS temporary directory. It does not use or alter an existing Aether service.
