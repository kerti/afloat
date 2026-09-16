# Testing strategy


Integration tests run against a **real Postgres via `testcontainers-go`**, started once per test
binary and shared by every test in it. Assertions are **stdlib `testing`** — no assertion library.
Unit tests with fakes stay for logic that has no database in it.

## Why a real database, from the first auth commit

Afloat's two most consequential rules are invisible to a fake:

- **Tenancy** (`BOOTSTRAP.md` §4) requires `household_id` in the `WHERE` clause of every
  Household-scoped query. A stub `Querier` returns whatever the test told it to and proves nothing.
  The only thing that can confirm a leak does not happen is a database holding two Households.
- **Auth** (§5.1) is almost entirely database behaviour: a session looked up by the SHA-256 of a
  token, an absolute lifetime read from `created_at`, a backoff row keyed by ip and email. Faking the
  store tests the fake.

Step 9 makes this worse, not better — every domain query is Household-scoped — so the harness is
built now, with auth as its first client, rather than retrofitted later.

## The shape, adopted from Balances (its ADR-0021)

That project litigated this and the reasoning transfers:

- **One container per test binary**, behind a `sync.Once`. Startup plus the migration run is paid
  once per package, not per test function.
- **Isolation by `TRUNCATE ... RESTART IDENTITY CASCADE` between tests, not a wrapping
  transaction.** The code under test is itself transactional and takes more than one connection from
  the pool; a `BEGIN`/`ROLLBACK` around it would mask or deadlock exactly the behaviour under test.
- **The table list comes from the catalog**, so a new migration's tables are swept with no test
  change.
- **Migrations are applied by the same goose runner and the same embedded FS the binary ships**, so
  tests run a bit-identical schema — not a hand-maintained copy that drifts.

## Assertions: stdlib only

Plain `if` and `t.Errorf`. No `testify`, and **no `google/go-cmp`**.

Balances' ADR-0021 names `go-cmp` for structural diffs, and that is worth reading carefully: the
dependency is **not in its `go.mod` and appears in zero of its test files**. The decision was
written down and never taken up. Recorded upstream as `kerti/balances-v2#667`.

The lesson is the reason this paragraph exists: *the decision that was litigated is the one in the
code, not the one in the document.* Afloat starts on plain stdlib because that is what actually
survived three years of that project, and adds `go-cmp` if and when a structural comparison is
genuinely painful — at which point it will be a decision made, not inherited.

## Lint

`golangci-lint` with `default: standard` (errcheck, govet, ineffassign, staticcheck, unused) plus
`revive`, and `gofmt` + `goimports` as formatters. `go test` runs with `-race`.

None of this was in 5a, which had `go vet` alone. Auth is the wrong code to ship unlinted: `errcheck`
on an ignored error and `staticcheck` on a misused `crypto/subtle` comparison are the kinds of thing
review misses and a linter does not.

## Consequences

- `backend/internal/testutil` owns `NewTestDB`. It needs docker; `make check-go` runs the full suite,
  so `make check` now needs docker for the backend surface.
- Tests within a package run sequentially. None call `t.Parallel`, because they share one database
  and truncate between.
- A test that needs two Households creates two. The harness does not pre-seed a tenant, because a
  pre-seeded one is a fixture everyone quietly depends on.
