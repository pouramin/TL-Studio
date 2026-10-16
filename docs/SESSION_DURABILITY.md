# Session durability qualification

This development work starts from `dev` at `3d529af` (PR #181). SQLite remains
opt-in with `TL_STUDIO_SQLITE_SESSIONS=1`. It is not a promotion to the stable
branch or a claim that physical power-loss testing is complete.

## Implemented safeguards

- Session metadata, messages, changes, pending-run records and operation IDs
  commit together. A known operation ID with identical input reconciles an
  ambiguous acknowledgment; it does not create a second commit.
- Process-kill tests stop creation, updates and deletion before/after commit.
  Migration has its own kill boundaries. A real native Agent test records a
  command side effect, kills the application and verifies recovery reports the
  interruption once without replaying the command.
- Migration checks every message payload and its order, session metadata,
  changes and pending state inside the import transaction. Divergent metadata,
  missing files, unreferenced archives or a failed source backup abort import.
- Before publishing a migration, every accessible legacy source is preserved in
  a private, byte-verified ZIP under `sessions/legacy-backups/`. The database
  retains a source manifest, counts, digest, backup path and truncation warnings.
  The JSON originals remain unchanged. Older completed migrations explicitly
  report when the detailed original report is unavailable.
- Once a SQLite authority file exists, this version refuses to write the old
  JSON authority with SQLite disabled. This prevents a silent switch back to
  stale pre-migration history. Keep the opt-in setting for that state directory.
- Ordinary SQLite Agent writes update the affected message and metadata without
  loading/re-encoding the whole history. Revision conflicts remain explicit.

## Backup and restore commands

Close the running application first. These commands acquire its state-directory
lock, do not start a browser or provider, and do not execute/replay pending tools.
They operate only on the opt-in SQLite session backend.

PowerShell, from the extracted **test** installation:

```powershell
$env:TL_STUDIO_SQLITE_SESSIONS = '1'
$env:TL_STUDIO_STATE_DIR = Join-Path $PWD 'test-state-sqlite'
.\tl-studio.exe --session-report
.\tl-studio.exe --session-backup (Join-Path $PWD 'session-backup-001.sqlite')

# Restore to a NEW state directory; never overwrite the only working history.
$env:TL_STUDIO_STATE_DIR = Join-Path $PWD 'restore-test-state'
.\tl-studio.exe --session-restore (Join-Path $PWD 'session-backup-001.sqlite')
.\tl-studio.exe --session-report
.\tl-studio.exe --project (Join-Path $PWD 'foundation-test-project')
```

The backup command uses SQLite's transactional `VACUUM INTO` snapshot, including
committed WAL data. It validates integrity, schema and foreign keys, flushes the
staged bytes and publishes a new file without replacing an existing destination.
It reports SHA-256, bytes and session/message/pending-run counts. See the
[SQLite snapshot contract](https://www.sqlite.org/lang_vacuum.html#vacuum_with_an_into_clause).
An interrupted backup can leave a private staging file; it must not be treated
as a successful backup. A directory-sync error can occur after publication: if
the command reports an error, validate/reconcile rather than overwriting/retrying
the same destination blindly.

Restore validates a private copy before publishing it. It rejects nonempty
session destinations and live-journal sources; use the generated backup rather
than a raw copy of `sessions.sqlite` while the app is running. Existing damaged
history and the source backup are preserved. Backups contain plaintext session
content, including embedded text attachments, and use private file permissions.
They do **not** include project files, provider credentials, plugins or other app
settings. Reconnecting a provider in a fresh restore test is expected; workspace
paths still refer to their original locations.

The publication primitive requires hardlinks on the destination's local
filesystem. NTFS and the supported local Unix filesystems are the qualification
targets. A filesystem that cannot publish safely returns an error; there is no
fallback that overwrites the only copy. SQLite WAL on network/synchronized
storage is not qualified. On Windows, file bytes are flushed but portable
directory fsync is unavailable; OS/filesystem/hardware power-loss behavior still
needs separate acceptance.

## Recorded local measurements

Five warm iterations per case on Windows amd64, Go 1.26.9, Ryzen 5 PRO 4650U:

| Native operation | 1,200 messages | 10,000 messages |
| --- | --- | --- |
| Read latest 200 | 1.55 ms / 339 KB allocated | 1.49 ms / 339 KB allocated |
| Update one message, before | 23.2 ms / 4.27 MB allocated | 129.6 ms / 41.10 MB allocated |
| Update one message, incremental path | 4.01 ms / 25 KB allocated | 5.16 ms / 26 KB allocated |

These small local samples are not cross-platform latency guarantees. Allocations
are per-operation bytes allocated, not retained memory or peak RSS. Full model
history reads and explicit whole-snapshot rewrites still scale with history size.

Reproduce with:

```sh
go test ./... -count=1
go test ./cmd/launcher -run '^$' -bench BenchmarkSQLiteNativeHistory -benchtime=5x -count=1
```

The 10,000-message test imports 49 archives plus the current tail, verifies every
message including attachment content, continues the history and restores a
verified backup without losing the oldest or newest entries. Native platform CI
records benchmark output; race CI also exercises the crash/migration tests.

Windows qualification also exposed a terminal shutdown race: a running shell
could spawn a child between the `taskkill` process-tree snapshot and termination.
Terminal commands now enter a kill-on-close Windows Job Object while suspended,
then resume. Descendants remain owned even if the parent exits. Immediate-close
stress coverage and an inherited-output-pipe regression exercise that cleanup;
other provider/MCP helper launch paths are unchanged.

## Remaining release gates

This covers deterministic process termination, not sudden physical power loss,
disk-full/device failure at every SQLite/VFS call, or a multi-day provider soak.
Legacy data discarded by older releases cannot be recovered. Uncooperative
filesystem writers and hostile path replacement need the separate workspace
integrity work. JSON remains the default for new state directories; its two-file
snapshot/index crash boundary is not redesigned by this patch. Do not change the
default or promote to `main` until those supported-platform acceptance decisions
are recorded.
