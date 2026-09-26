# Cross-backend conformance harness

One suite, run against both backends, asserting they answer identically. Issue
[#28](https://github.com/kerti/afloat/issues/28); milestone **Base parity**.

`make check` proves each backend is well-formed. Nothing else proves they are
the *same* — every divergence found so far (#18 through #33) was found by a
person reading two codebases, because no test runs against both. That does not
survive the domain, where the divergences become arithmetic.

All three steps are done: the runner, the case format and the boot script;
a case for every endpoint, every reachable `ErrorCode`, the session cookie
attribute by attribute, the rows each call leaves behind, and the path shapes
and body cap ruled in #56; and the `conformance` job in
`.github/workflows/ci.yml`, a required check on `main`.

Not covered, on purpose: the Argon2 concurrency cap (#33) has no observable
answer to assert without a load test, and #30's handler timeout is internal and
differs by design (§12).

## Running it

```sh
make conformance          # builds both backends, boots them, runs the suite
```

It needs docker (for Postgres), a Go toolchain and a JDK. The script owns the
whole lifecycle: one database per backend from `db/migrations`, a pair of
servers per [profile](#profiles) started as plain processes on their §1 ports,
a readiness wait on `GET /api/health`, and a teardown that prints every log if
anything failed. A full run takes about ten seconds once both are built.

**It empties both dev databases.** Every case truncates every Afloat table in
`afloat_go` and `afloat_kotlin` and applies the seed, so whatever a local run of
either backend left there is gone afterwards.

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
| `AFLOAT_GO_DATABASE_URL` | `postgres://afloat:afloat@localhost:5184/afloat_go` | The Go backend's database, which the runner resets, seeds and reads |
| `AFLOAT_KOTLIN_DATABASE_URL` | `postgres://afloat:afloat@localhost:5184/afloat_kotlin` | The Kotlin backend's |
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
   names which one. A `given` step that does not land fails the case the same
   way, naming the backend and the step.
2. **The two agree with every assertion and still differ** — reported under
   `parity`. Neither is necessarily wrong: **the case file is incomplete**, and
   that is its own finding.

Mode 2 is why this exists. It compares the *whole* answer — status, every
header, the body bytes — not only what a case thought to assert, which is what
would have caught every issue in this milestone. And it compares the *whole*
database: every row of every Afloat table each backend left behind (see
[persisted state](#persisted-state)).

## The case format

`cases/*.yaml`, a list per file. Hand-written, from `contract/openapi.yaml` and
the decisions in this milestone.

```yaml
- name: health reports ok when the database is reachable
  issue: "26"            # optional: the decision this case pins
  given:                 # optional: steps run first, in order (see below)
    - request: {method: POST, path: /auth/local/login, headers: {...}, body: '...'}
      status: 204        #   required: a setup step that silently failed
                         #   would make the case about the wrong state
    - sql: UPDATE sessions SET expires_at = now() + interval '1 hour'
    - outage: true       #   the database goes away until the request is answered
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
    cookies:             # optional: every Set-Cookie, attribute by attribute
      afloat_session:    #   `cookies: {}` asserts that none is set
        value_pattern: '[A-Za-z0-9_-]{43}'   # or value: "" for an exact value
        path: /
        max_age: 2592000
        http_only: true
        secure: false
        same_site: Lax
        expires: max-age # absent | past | max-age (Date + Max-Age)
    rows:                # optional: queries against the backend's database,
      - sql: SELECT count(*)::int AS n FROM sessions  # run after the request
        rows: [{n: 1}]
  profile: default       # optional: which booted pair it runs against
  permit: []             # optional: case-scoped permitted differences, by name
```

`body_json`, `body_raw` and `body_empty` are mutually exclusive, a case with no
`status` is rejected, and two cases may not share a name. A hand-written case
file's likeliest defect is an empty `expect` that passes against any answer at
all, so the loader refuses one. It also refuses a cookie expectation that
leaves any attribute but `domain` unpinned (an absent `domain` asserts a
host-only cookie), a `rows` entry with no `rows:` list (write `rows: []` to
assert none), a request step with no `status`, and an `outage` that is not the
last `given` step.

A header set to `""` is not sent. That is net/http's rule for `User-Agent`, and
it means a case cannot send an empty-valued header; #32 item 4 is pinned in each
backend's own suite for that reason. `Host` sets the request's host rather than
a header, so a case that compares `Origin` with `Host` means the same thing on
two backends on two ports.

### Every case starts from the seed

Before each case, on each backend, the runner truncates every Afloat table
(discovered, not listed, so a new migration's table is covered the day it
lands) and applies `fixtures/seed.sql`: one Household, a User who can sign in,
one with no credential, and a soft-deleted one whose credential would verify.
Each backend gets its own cookie jar per case. No case depends on another, and
the order they run in means nothing.

The seeded password's hash is one Argon2id PHC string at the §5.1 cost, used on
both backends, which each verify it by the parameters inside it.

### Given steps

`given` is how a case reaches a state without asserting on the way there:
signed in, throttled, a session near its expiry. A `request` step goes through
the case's cookie jar, so a login's cookie is presented by everything after it.
A `sql` step runs against that backend's own database, for what no request can
do — moving a session's `expires_at` into the past rather than waiting for it.

An `outage` step makes the database unreachable from the backend: the runner
sets `ALLOW_CONNECTIONS false` on it and terminates every connection it holds,
sparing only its own. The database comes back once the request is answered,
and the runner waits for `/health` to say 200 before the next case — a pool
rebuilds in its own time, and a case must not fail for the one before it.

### Cookies

`Set-Cookie` is the one header parity does not compare as bytes. Two parts of
it differ by construction and are compared by meaning instead:

- **the value**, when minted: parity compares its length and whether it is
  empty; the case's `value_pattern` pins its shape on each backend.
- **Expires**: parity compares whether it is sent, whether it is in the past,
  and otherwise how far past `Date` it sits, to within two seconds. Spring
  writes the day of the month unpadded and reads its own clock; neither is a
  difference.

Every other attribute — name, `Path`, `Domain`, `Max-Age`, `HttpOnly`,
`Secure`, `SameSite`, and any attribute no one expected — must match exactly.

### Persisted state

After the request, and before any `same_as` request, the runner snapshots every
Afloat table on each backend's database and compares the two. Timestamps are
compared as their distance from the snapshot's own `now()`, to within two
seconds: the two runs happen seconds apart, so what must match is how old a row
is and how far ahead an expiry sits, not the instant. Every other column is
compared by its text form, so `NULL` and `''` differ, as do `1` and `1.0000`.

The migration runners' own tables (`goose_db_version`, `flyway_schema_history`)
are skipped, since they differ by design. A column neither backend can
reproduce from the other's is skipped by a `column:` entry in the
permitted-difference list.

`expect.rows` is the assertion half: a query and the rows it must return, on
each backend. Cast every column to text, an integer or a boolean — anything else
is refused rather than compared by how a driver renders it.

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

**`expect.same_as_for`** is `same_as` restricted to one backend (`go` or
`kotlin`), for a case whose two backends are permitted to differ so much
(`status_by_backend`, below) that the relation cannot hold on both at once:
Go's answer to a malformed path IS its own unregistered-path 404, but Kotlin's
connector-level 400 is not Kotlin's own 404. A case may set `same_as`,
`same_as_for`, both (for different backends), or neither — but a `same_as` or
`same_as_for` identical to the case's own `request` is rejected at load time,
since it would only ever compare an answer to itself.

**Not generated from the OpenAPI spec, on purpose.** The spec says what a
response *may* look like; a case says what it *is* — which status a specific bad
body produces, what the envelope's `code` is, what the cookie's attributes are.
Generating these would re-derive the ambiguity the harness exists to remove.

### When the two backends are permitted to answer different statuses

`expect.status` is a single value both backends must match — the ordinary
case. `expect.status_by_backend` (a `{go: ..., kotlin: ...}` map, both keys
required, and the two values must actually differ) is its alternative for a
case-scoped permitted difference whose whole point is that the backends
*don't* agree on status (`status: true` on the entry, #64's connector-vs-router
404-or-400 split and #66's `OPTIONS *` are the two rulings that needed this).
The two are mutually exclusive, and using `status_by_backend` requires citing a
permit entry with `status: true` — the loader rejects a case that sets one
without the other.

### `raw_target`: bytes url.Parse would otherwise normalise away

`request.path` goes through Go's own `http.NewRequest`, which silently fixes
up anything the URL type doesn't like — an unescaped backslash becomes `%5C`,
for instance — before the request ever reaches either server. `request.raw_target`
is `path`'s alternative for a case that needs the literal bytes on the wire:
sent verbatim as the request line's target via `URL.Opaque`, never escaped,
never re-parsed. Relative to the API base and takes a leading slash the same
way `path` does, with two exceptions that are not nested under the base at
all: the bare asterisk-form `*` (RFC 9110 §7.1's `OPTIONS *`, and any
asterisk-form variant such as `*?x=1`) and an absolute-form target (a full
URI, such as `http://x*`) — recognised by a leading `*` or a `://` anywhere in
the value.

### `headers_hex`: header values that are not valid UTF-8

`request.headers` is a YAML string, which is Unicode text — it cannot hold an
arbitrary byte sequence, and a `\xHH` escape inside a quoted YAML scalar means
the Unicode code point U+00HH (encoded as however many UTF-8 bytes that takes),
never the raw byte `0xHH`. `request.headers_hex` is `headers`' alternative for
exactly that case (#70's invalid-UTF-8 `sessions.user_agent` rows): each value
is hex, decoded and sent as the header's exact bytes. A name may appear in only
one of `headers` or `headers_hex` (checked case-insensitively, since HTTP
header names are), and `Host` is rejected from `headers_hex` outright — `path`
gives it its own connection-level meaning (`req.Host`, never a literal header),
which raw bytes have no equivalent for. This client's own header-value check
still applies even writing bytes directly: it refuses NUL, `0x01`, `0x1B`,
`0x7F` and a bare CR or LF, and allows everything else, HTAB and any UTF-8/C1
byte (`0x80`–`0xFF`) included — a case naming one of the refused bytes needs a
raw socket instead (`obs_fold_test.go`, `ErrorDispatchSpec.kt`'s Kotlin
equivalent), not this harness.

## The permitted-difference list

`permitted-differences.yaml`. Axis 1 of the milestone is "identical observable
responses … plus a documented and tested list of permitted differences"; that
file is the list, and the runner reads it, so prose and enforcement cannot drift
apart.

Three kinds of entry. A **global** one names a `header` and applies to every
case. A **case-scoped** one has a `name`, lists any of `headers`, `body` or
`status`, and applies only to cases that cite it under `permit:` — for a
difference that is right on those answers and would be a bug anywhere else. A
**column** one names a `table.column` the persisted-state comparison skips on
every case. A scoped entry is always a ruling, so it always needs an `issue`.
The loader and `go test -short` refuse a case that cites an undefined entry, a
scoped entry no case cites, a case that permits a body difference without
pinning the body another way (`same_as`, `same_as_for`, or a body assertion —
and if `same_as_for` is the *only* one, it must carry the `go` key: Go's side
is the one that is reliably self-consistent), and a case that permits a status
difference without `status_by_backend`.

Every entry needs a `why`. An entry that encodes a **decision** also needs its
`issue`, and `provisional: true` marks one that exists only because the decision
has not landed — the runner prints those on every run, so the list cannot
quietly calcify into "the backends differ and nobody minds".

Adding a header here to make a red run green, with no issue behind it, is hiding
a divergence rather than ruling on one.

Today the list holds three permanent global entries — `Date` and
`Content-Length`, both properties of HTTP rather than decisions, and
`X-Request-Id` (#26: both backends send it, but a minted id is random; the
sanitising rule is pinned by `cases/request-id.yaml` instead) — and several
case-scoped entries: `unmatched-path-404` (#24: each backend keeps its own
unregistered-path 404, so the frontend must act on a non-envelope 404's status
alone); `malformed-path-connector-400` (#64: a handful of path shapes, and a
non-OPTIONS method with an asterisk-form target, that Tomcat's connector
refuses outright where Go's router simply finds no route); and `options-star`
(#66: the asterisk-form and absolute-form targets that OPTIONS's own dispatch,
not path routing, treats differently on the two connectors). #26 retired the
provisional entries: the six security headers are identical on both backends
and compared like any other, and neither backend sends
`Strict-Transport-Security`. One column entry, `sessions.id`: the hash of a
random token, which two backends never share.

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
  and gives failures that mean two things at once. The runner connects to both
  and compares every table after every case.
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
