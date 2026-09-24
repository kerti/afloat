# Cross-backend conformance harness

One suite, run against both backends, asserting they answer identically. Issue
[#28](https://github.com/kerti/afloat/issues/28); milestone **Base parity**.

`make check` proves each backend is well-formed. Nothing else proves they are
the *same* — every divergence found so far (#18 through #33) was found by a
person reading two codebases, because no test runs against both. That does not
survive the domain, where the divergences become arithmetic.

This is **step 1** of three: the runner, the case format, the boot script, and
one case. Step 2 adds the rest of the cases as the Wave 1 decisions land — a
case written before its decision pins today's accident rather than tomorrow's
answer. Step 3 wires it as a CI job and adds it to branch protection.

## Running it

```sh
make conformance          # builds both backends, boots them, runs the suite
```

It needs docker (for Postgres), a Go toolchain and a JDK. The script owns the
whole lifecycle: one database per backend from `db/migrations`, a pair of
servers per [profile](#profiles) started as plain processes on their §1 ports,
a readiness wait on `GET /api/health`, and a teardown that prints every log if
anything failed.

To drive an already-running pair by hand:

```sh
cd contract/conformance
AFLOAT_REQUIRE_CONFORMANCE=1 go test -v ./...
```

| Variable | Default | Meaning |
|---|---|---|
| `AFLOAT_GO_BASE_URL` | `http://localhost:5182/api` | Where the Go backend is |
| `AFLOAT_KOTLIN_BASE_URL` | `http://localhost:5183/api` | Where the Kotlin backend is |
| `AFLOAT_GO_LOCAL_DISABLED_BASE_URL` | `http://localhost:5186/api` | Go, `local-disabled` profile |
| `AFLOAT_KOTLIN_LOCAL_DISABLED_BASE_URL` | `http://localhost:5187/api` | Kotlin, `local-disabled` profile |
| `AFLOAT_REQUIRE_CONFORMANCE` | unset | `1` turns "no backend answering" from a **skip** into a **failure** |

The skip is deliberate and so is the way to remove it: same shape as
`AFLOAT_REQUIRE_TEST_DB` in both backends' suites. A machine with the backends
up must never report green on a run that never happened.

`go test -short ./...` runs the case-file and allowlist tests and nothing that
needs a server. That is what `make check` uses, so the committed files are
validated by the pre-push gate without the gate depending on two live backends.

## The two failure modes, and why they are reported separately

1. **A backend disagrees with the expected answer** — reported under
   `expected/go` or `expected/kotlin`. That backend is wrong, and the subtest
   names which one.
2. **The two agree with every assertion and still differ** — reported under
   `parity`. Neither is necessarily wrong: **the case file is incomplete**, and
   that is its own finding.

Mode 2 is why this exists. It compares the *whole* answer — status, every
header, the body bytes — not only what a case thought to assert, which is what
would have caught every issue in this milestone.

## The case format

`cases/*.yaml`, a list per file. Hand-written, from `contract/openapi.yaml` and
the decisions in this milestone.

```yaml
- name: health reports ok when the database is reachable
  issue: "26"            # optional: the decision this case pins
  request:
    method: GET
    path: /health        # relative to the /api base, which the base URL carries
    headers: {}
    body: ""             # sent verbatim, so a case can send invalid JSON on purpose
  expect:
    status: 200
    headers: {}          # compared exactly, by value
    body_json: {...}     # parsed and deep-compared; key order does not matter
    # body_raw: "..."    # or: the bytes themselves are the assertion
    # body_empty: true   # or: assert a zero-length body, as every 204 requires
    # same_as:           # or alongside: this answer must equal the backend's
    #   method: GET      #   own answer to a second request (see below)
    #   path: /no-such-route
  profile: default       # optional: which booted pair it runs against
  permit: []             # optional: case-scoped permitted differences, by name
```

`body_json`, `body_raw` and `body_empty` are mutually exclusive, a case with no
`status` is rejected, and two cases may not share a name. A hand-written case
file's likeliest defect is an empty `expect` that passes against any answer at
all, so the loader refuses one.

### Profiles

A profile is one server configuration, and `scripts/conformance.sh` boots a
pair of backends for each. A case runs against its own profile only — a case
with none runs against `default` — so the login cases to come are never also
run against a pair where login is off.

| Profile | Differs from `default` | Go | Kotlin |
|---|---|---|---|
| `default` | — | `5182` | `5183` |
| `local-disabled` | `AUTH_LOCAL_ENABLED=false`, `AUTH_GOOGLE_ENABLED=true` (a backend refuses to boot with no provider), `AUTO_MIGRATE=false` | `5186` | `5187` |

The second pair shares each backend's database with its default twin and starts
only once the default pair has migrated it. Compose creates just the two
databases, and nothing the `local-disabled` cases do writes a row. Only the
profiles some case uses are waited for.

Adding a profile means a constant in `case.go`, a pair in `conformance_test.go`,
an `*_env` function and two `start` lines in the script, and two §1 ports.

### `same_as`: asserting a relation instead of bytes

`expect.same_as` is a second request sent to the **same** backend; the case's
answer must equal it in status, every header, and the body bytes. `Date` and
`X-Request-Id` are skipped, since they are minted per response. A mismatch is
reported under `expected/<backend>`, because it means that backend is wrong
whatever the other one does.

It exists for rulings that are relations. #24 says a disabled login route
answers *exactly like an unregistered path* — on each backend, where the two
backends' unregistered-path 404s are themselves permitted to differ. A byte
assertion would fail one of them; `same_as` pins the ruling itself.

**Not generated from the OpenAPI spec, on purpose.** The spec says what a
response *may* look like; a case says what it *is* — which status a specific bad
body produces, what the envelope's `code` is, what the cookie's attributes are.
Generating these would re-derive the ambiguity the harness exists to remove.

## The permitted-difference list

`permitted-differences.yaml`. Axis 1 of the milestone is "identical observable
responses … plus a documented and tested list of permitted differences"; that
file is the list, and the runner reads it, so prose and enforcement cannot drift
apart.

Two kinds of entry. A **global** one names a `header` and applies to every case.
A **case-scoped** one has a `name`, lists `headers` and/or `body`, and applies
only to cases that cite it under `permit:` — for a difference that is right on
those answers and would be a bug anywhere else. A scoped entry is always a
ruling, so it always needs an `issue`. The loader and `go test -short` refuse a
case that cites an undefined entry, a scoped entry no case cites, and a case
that permits a body difference without pinning the body another way
(`same_as` or a body assertion), since parity would otherwise go blind on it.

Every entry needs a `why`. An entry that encodes a **decision** also needs its
`issue`, and `provisional: true` marks one that exists only because the decision
has not landed — the runner prints those on every run, so the list cannot
quietly calcify into "the backends differ and nobody minds".

Adding a header here to make a red run green, with no issue behind it, is hiding
a divergence rather than ruling on one.

Today the list holds two permanent global entries — `Date` and
`Content-Length`, both properties of HTTP rather than decisions — one
case-scoped entry, `unmatched-path-404` (#24: each backend keeps its own
unregistered-path 404, so the frontend must act on a non-envelope 404's status
alone), and **eight provisional ones, all
pending [#26](https://github.com/kerti/afloat/issues/26)**: `X-Request-Id`,
which Kotlin sends and Go does not, and the seven Spring Security defaults
(`X-Content-Type-Options`, `X-Frame-Options`, `X-XSS-Protection`,
`Cache-Control`, `Pragma`, `Expires`, `Strict-Transport-Security`). They are
listed one per header rather than as a wildcard so #26 has to answer for each.

## Design decisions, and what they rejected

Settled on #28 before any of this was written.

- **A Go test binary, in its own module** (`github.com/kerti/afloat/conformance`),
  importing neither backend. A separate `go.mod` makes that structural rather
  than a rule someone remembers: if this ever imports `backend/internal/...`,
  the thing being tested has leaked into the thing doing the testing. Rejected:
  hurl and schemathesis (a fourth toolchain, and neither asserts persisted
  rows); `bash` + `jq` (no structure to grow into); putting it in either
  backend's suite, where it would silently become that backend's spec.
- **One database per backend, compared explicitly.** A shared database makes
  case ordering significant, makes one backend's writes the other's fixtures,
  and gives failures that mean two things at once. Row comparison arrives with
  the cases that need it, in step 2.
- **Hand-written expected answers.** Recording Go's answers and diffing Kotlin
  against them would make Go canonical by construction — which it is — but would
  also silently bless Go's bugs, of which this milestone has several.
- **Built and run as plain processes**, not `go run` / `./gradlew bootRun`. Both
  of those wrap the server in a parent that outlives a kill of itself, leaving a
  port bound and the next run failing for a reason unrelated to the code.

## What this does not do

It does not replace either backend's suite. The Kotlin specs and the Go tests
stay exactly as they are: they test *how* each backend works and run in seconds.
This runs slower, on demand and in CI, and tests only *what comes out*.
