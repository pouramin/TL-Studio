# Foundation decision: transactional Session authority (v0.7)

**Status:** architecture decision / IMPLEMENTATION NOT DONE. Blocking PR #178 acceptance.
**Baseline:** dev `05efaaaf7965b39a834a1d12861d885cb6234275`; working PR #178 `hardening/foundation-acceptance-070`. No feature work and no changes to `main`.

## Incident and current invariants

An independent isolated audit reproduced failures despite the PR #178 staged snapshot + index + compensating rollback:
- index failure followed by failed snapshot restoration exposes new messages with old metadata;
- process termination between snapshot and index replacements leads to old index with rejected new snapshot content after restart;
- startup recovery can promote an uncommitted pending-run message into durable history;
- archive files can be written before the snapshot/index commit boundary;
- an unindexed new-session snapshot may be readable or later reused.

The existing green failure-injection tests cover snapshot failure and index failure with successful compensation, not crashes or compensation failure. **Do not declare two-file rollback code production-safe.** It must be replaced, not extended with additional retries or swallowed errors.

## Decision: SQLite as the candidate single Session authority

Use **one SQLite database** for Session metadata, messages (including archived history), changes, pending Agent run markers and commit versions. Commit all logically related rows in **one transaction**; a transaction is the sole visibility boundary. Reads for list/get/messages/changes must use the same committed authority rather than separately cached JSON index and snapshot files.

Session memory may cache committed rows only, with invalidation/versioning and failure handling; it cannot become an independent source of truth. Avoid a separate writable Session index or archives that change outside the transaction. If message chunks are kept as immutable external blobs for size reasons, write and sync those **before** the DB transaction and expose them only through committed manifest rows; unreachable blobs are garbage-collectable and cannot affect reads. Prefer storing all relevant history inside SQLite initially, to avoid another split authority.

Prototype a CGo-free SQLite Go driver first to preserve current CGO-disabled Windows/Linux/macOS cross-builds (candidate: `modernc.org/sqlite`); verify dependencies, binary size, Windows builds and performance in a disposable migration branch/working tree that remains part of PR #178. Do not treat a driver selection as validated without CI. Avoid linking a CGo-only driver without an explicit build/deployment decision.

Durability target: local filesystem, transactions, `journal_mode=WAL` and `synchronous=FULL`, a per-connection foreign key setting and bounded busy-timeout. Verify the actual active pragmas in integration tests, and control connections, checkpointing and shutdown. WAL is not generally appropriate for cross-host network filesystems; detect/disallow unsupported locations or document a tested alternative. Acknowledged commits rely on filesystem and hardware honoring sync semantics. SQLite handles database transaction atomicity; **it cannot atomically commit arbitrary external shell commands or workspace edits**.

## Migration without historical loss

1. Keep legacy JSON state unmodified and make a durable backup before conversion; never overwrite/delete legacy files on a partial migration.
2. Load every indexed session, its referenced snapshot, all history archives (including archived batches), changes, runtime/pending state and metadata, preserving order and message IDs. Report missing/corrupt snapshots separately and never silently discard them.
3. Migrate using a single DB transaction (or an explicitly resumable series with durable migration state) into a new database. Validate counts, IDs, order, message bodies, archive coverage and referential integrity against the legacy source before switching reads.
4. On migration failure, leave old data intact, abort startup or offer an explicit recovery path; **never** start an empty store and silently replace history.
5. Perform the cutover only after successful verification; retain legacy backup until an explicit separately tested cleanup policy is approved. A rollback to the old application must not silently mix new and old authorities.
6. Test migration multiple times against real-format synthetic fixtures including 1,200+ messages, archives, pending runs, duplicate and corrupt data, and simulated interruption at each stage.

## Acceptance criteria and tests

- Fault injection for BEGIN, DML, COMMIT, busy/locked DB, I/O error and corruption; no partial list/message/metadata visibility on failed transaction.
- Real child-process crash tests (kill at defined transaction transitions), clean restart, concurrent readers and writers; either whole previous generation or whole committed new generation is visible.
- Tests for new Session, update, deletion, accepted-run journaling, completion, archive transition, interruption and restart. No rejected content is promoted into durable history.
- After a successful COMMIT but before caller receives its result, the operation may have committed despite uncertain acknowledgment. Use stable operation/message IDs and idempotent retry or reconciliation; **never** promise that missing acknowledgment proves rollback.
- No automatic replay of terminal commands or file modifications whose outcome is uncertain at crash time.
- Full `go test ./...` and native Windows/macOS/Linux CI, plus migration validation, `PRAGMA integrity_check`, long history performance and Windows manual acceptance.
- Any remaining legacy state writers (credentials, permissions, provider settings) get a separate inventory and failure-injection policy; Session transaction work does not silently change their semantics.

## Related release blockers: local API and workspace

**API:** Existing loopback Host/Origin checks are not native-client authentication. Before release, a launch-scoped authorization boundary must cover every sensitive local route. A URL-fragment bootstrap passed on the browser process command line is **not sufficient to protect against another OS user** who can inspect process arguments. Prefer an OS-identity-bound bootstrap or a user-entered code in shared-host contexts until an automatic mechanism is demonstrably private; do not offer a 60-second stolen bearer code as an adequate mitigation. Native EventSource needs cookie or ticket authentication; same-host different-port preview is not isolated by SameSite cookie settings alone. Test browser reload/tabs, `--no-browser`, Windows CLI and Chrome extension, which uses a separate pairing mechanism.

**Workspace:** Expected SHA followed by ordinary rename cannot guarantee that an uncooperative external writer cannot change the file in the gap. A Linux `renameat2(RENAME_EXCHANGE)` prototype is not a verified portable contract; the proposed Windows share-mode scheme is unverified. Test open-handle writers, symlinks/hardlinks, ACLs/permissions, network drives, rename failures, and persistent backups. Never claim conflict-free writes on unsupported filesystems. Preserving displaced and proposed versions is a recovery strategy, not an unconditional atomic compare-and-swap guarantee.

## Independent design challenge: measured workload and storage engine choice

A further isolated review reported about 6 ms / 0.20 MB for 70 messages, 22 ms / 0.64 MB for 220, 43 ms / 1.8 MB for 620, and 99 ms / 3.0 MB for 1,220 per whole-snapshot rewrite. These are *reviewer-reported sandbox benchmarks*, not verified cross-platform performance numbers. A second writer opening the same per-user state directory with an earlier cached index could drop another launcher's Session from the listing. There is no cross-process session lock in the legacy JSON design.

### Rejected alternative: bespoke append-only per-session logs (Option B-prime)

A per-session framed append-only log with checksums, torn-tail recovery, checkpoint generations, immutable blobs, cross-process locks and an index rebuilt from headers can provide a single commit point and O(new-data) writes. It avoids a SQL dependency. **It also makes TL Studio responsible for a new crash-safe storage engine**: compaction atomicity, frame codec fuzzing, middle-file corruption, synchronization and multi-process recovery would all be security/reliability-critical code owned by this project. That is the strongest reason not to select it for the current reliability-focused foundation. Green unit tests alone cannot offset the maturity difference. Preserve this option as a documented trade-off, not a parallel implementation.

### Retained decision: SQLite with explicit driver/toolchain qualification

Choose SQLite as one authoritative source for Session metadata, messages, changes, pending runs and history; one transaction for each logical update, with reads using committed DB state. SQL transactions must use stable operation IDs and guard against concurrent/stale overwrites as well as cross-process serialization. Favor incremental message rows over rewriting entire conversation JSON.

Two current CGo-free libraries are `modernc.org/sqlite` and `github.com/ncruces/go-sqlite3`. Recent versions require Go 1.26 or newer; this is a **toolchain compatibility issue, not proof no compatible driver release exists**: for example `ncruces/go-sqlite3` v0.23.0 declares Go 1.22, and v0.25.0 declares Go 1.23 (with Go 1.24 toolchain suggested). Both old-driver pinning and an upgrade to a supported Go release require a security, portability and support decision. Go 1.23 is no longer among Go's two currently supported major releases as of October 2026. Prefer qualifying a currently supported Go toolchain (at least Go 1.26) and a maintained driver, rather than permanently pinning an old runtime to avoid a one-time migration. **Neither choice is implemented or benchmark-accepted yet.**

For any driver, record pinned version, full transitive dependency audit, license, binary delta, cold-start and 1,200+/10,000+ message benchmarks, Windows/macOS/Linux CGO-disabled CI compatibility, WAL pragmas, database busy handling and crash-matrix results before introducing it into production.

### Legacy history loss is already possible

The v0.6.2 release source explicitly clips persisted messages beyond 1,200 (`session_persistence.go` lines 159-160) and drops oldest quarters for snapshots above 32 MiB (near line 207). Migration cannot recreate data that was previously overwritten. Preserve *every accessible* message and archive, flag legacy Sessions near the old limit as **possibly previously truncated**, and avoid promising recovery of irretrievable history. The dev preview additionally uses `ArchivedBatches` and `PendingRun` while retaining persistence version 1; migration must discriminate actual file content rather than trusting that version alone.

The source of truth for migration should be a comprehensive read-only legacy-format importer with explicit reports for missing snapshots, corrupt archives, metadata discrepancies and questionable truncation. A legacy-preserving migration must refuse silent partial imports, make the new authority switch explicit and ensure a downgrade cannot silently edit a stale legacy copy or pretend to show post-migration history.

## Release decision

Until the transactional Session authority, secure local API access and a candid supported-platform workspace integrity contract pass the acceptance suite, **#178 remains Draft and v0.7 is no-go**. Do not merge to dev, promote to main, or introduce new product features. All stabilization work remains in the same PR.
