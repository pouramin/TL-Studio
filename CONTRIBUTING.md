# Contributing

Develop from `dev` and open pull requests against `dev`. `main` is the stable
release branch; development fixes must not be pushed or merged there directly.

Use Go 1.26.9 or a newer supported security patch and Node 22+. Run:

```sh
npm ci --ignore-scripts
npm run check:web
npm run build:web
go test ./... -count=1
go vet ./...
python3 scripts/check-release-auth-contract.py
```

Linux CI additionally runs the race detector and the Go/npm vulnerability
checks. Use temporary projects and a temporary TL_STUDIO_STATE_DIR for runtime
tests. Never run acceptance tests against a user's important workspace.

Add behavior tests for provider protocol changes, permission boundaries, state
recovery, and filesystem identity. Test complete and truncated streams, not just
JSON responses. Never retry ambiguous mutating tool outcomes automatically.

Current ownership boundaries are described in docs/ARCHITECTURE.md. Prefer
small typed interfaces between auth, session persistence, process lifecycle,
provider transports, and Browser state. Broad package extraction is a separate
incremental refactor and must preserve the regression suite.

SQLite recovery, verified backup/restore commands, migration acceptance and
long-history measurements are documented in [SESSION_DURABILITY.md](docs/SESSION_DURABILITY.md).
