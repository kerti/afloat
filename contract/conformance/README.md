# Conformance harness

A black-box suite of HTTP cases, run against the real backend and its real
database. Issue [#28](https://github.com/kerti/afloat/issues/28).

`make check` proves the backend is well-formed, and its own suite tests *how*
it works. This tests only *what comes out*: the answer a real server gives over
a real socket, and the rows it leaves behind, case by case, as written from
`contract/openapi.yaml` and the rulings behind it.

Covered: every endpoint, every reachable `ErrorCode`, the session cookie
attribute by attribute, the rows the stateful calls leave behind (where a case asserts `rows`), and the path shapes
and body cap ruled in #56. CI runs it as the `conformance` job in
`.github/workflows/ci.yml`.

Not covered, on purpose: the Argon2 concurrency cap (#33) has no observable
answer to assert without a load test.

## Running it

```sh
make conformance          # builds the backend, boots it, runs the suite
```

It needs docker (for Postgres) and a Go toolchain. The script owns the whole
lifecycle: one server per [profile](#profiles), started as a plain process on
its §1 port against the `afloat` database, a readiness wait on
`GET /api/health`, and a teardown that prints every log if anything failed.

**It empties the dev database.** Every case truncates every Afloat table in
`afloat` and applies the seed, so whatever a local run left there is gone
afterwards.

To drive already-running servers by hand:

```sh
cd contract/conformance
AFLOAT_REQUIRE_CONFORMANCE=1 go test -v ./...
```

| Variable | Default | Meaning |
|---|---|---|
| `AFLOAT_BASE_URL` | `http://localhost:5182/api` | Where the `default` profile's server is |
| `AFLOAT_LOCAL_DISABLED_BASE_URL` | `http://localhost:5186/api` | The `local-disabled` profile's server |
| `AFLOAT_FOUNDING_DISABLED_BASE_URL` | `http://localhost:5187/api` | The `founding-disabled` profile's server |
| `AFLOAT_DATABASE_URL` | `postgres://afloat:afloat@localhost:5184/afloat` | The database, which the runner resets, seeds and reads |
| `AFLOAT_REQUIRE_CONFORMANCE` | unset | `1` turns "no server answering" from a **skip** into a **failure** |

The skip is deliberate and so is the way to remove it: same shape as
`AFLOAT_REQUIRE_TEST_DB` in the backend's suite. A machine with the backend up
must never report green on a run that never happened.

`go test -short ./...` runs the case-file tests and nothing that needs a
server. That is what `make check` uses, so the committed files are validated by
the pre-push gate without the gate depending on a live backend.

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
    body_json: {...}     # parsed and deep-compared; key order does not matter.
                         #   A string "<re:REGEXP>" matches any string the whole of
                         #   which REGEXP matches (see below)
    # body_raw: "..."    # or: the bytes themselves are the assertion
    # body_empty: true   # or: assert a zero-length body, as every 204 requires
    # same_as:           # or alongside: this answer must equal the server's
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
    rows:                # optional: queries against the database,
      - sql: SELECT count(*)::int AS n FROM sessions  # run after the request
        rows: [{n: 1}]
  profile: default       # optional: which booted server it runs against
```

`body_json`, `body_raw` and `body_empty` are mutually exclusive, a case with no
`status` is rejected, two cases may not share a name, and an unknown key is
refused rather than ignored. A hand-written case
file's likeliest defect is an empty `expect` that passes against any answer at
all, so the loader refuses one. It also refuses a cookie expectation that
leaves any attribute but `domain` unpinned (an absent `domain` asserts a
host-only cookie), a `rows` entry with no `rows:` list (write `rows: []` to
assert none), a request step with no `status`, and an `outage` that is not the
last `given` step.

A header set to `""` is not sent. That is net/http's rule for `User-Agent`, and
it means a case cannot send an empty-valued header; #32 item 4 is pinned in the
backend's own suite for that reason. `Host` sets the request's host rather than
a header, so a case that compares `Origin` with `Host` means the same thing on
any port.

### Every case starts from the seed

Before each case, the runner truncates every Afloat table
(discovered, not listed, so a new migration's table is covered the day it
lands) and applies `fixtures/seed.sql`: one Household, a User who can sign in,
one with no credential, and a soft-deleted one whose credential would verify.
Each case gets its own cookie jar. No case depends on another, and the order
they run in means nothing.

The seeded password's hash is one Argon2id PHC string at the §5.1 cost, which
the backend verifies by the parameters inside it.

### Given steps

`given` is how a case reaches a state without asserting on the way there:
signed in, throttled, a session near its expiry. A `request` step goes through
the case's cookie jar, so a login's cookie is presented by everything after it.
A `sql` step runs against the database, for what no request can
do — moving a session's `expires_at` into the past rather than waiting for it.

An `outage` step makes the database unreachable from the server: the runner
sets `ALLOW_CONNECTIONS false` on it and terminates every connection it holds,
sparing only its own. The database comes back once the request is answered,
and the runner waits for `/health` to say 200 before the next case — a pool
rebuilds in its own time, and a case must not fail for the one before it.

### Cookies

`expect.cookies` holds every `Set-Cookie` to the case attribute by attribute,
not as bytes. A minted value is matched by `value_pattern`; `Expires` is
compared by meaning (`absent`, `past`, or `max-age`: Date plus Max-Age, to
within two seconds). Any attribute the case does not expect is a failure.

### `<re:...>`: an answer that carries the server's own date

`body_json` is compared exactly, key for key. A string of the form
`"<re:REGEXP>"` is the one exception: it matches any string the whole of which
the regexp matches, and nothing else in the body is loosened. It exists for the
Budget (#130), whose answer names the Budget Period Today falls in: Today comes
from the server's clock, which a black-box case cannot pin, so the case states
the *shape* of the dates (`"<re:[0-9]{4}-[0-9]{2}-01>"`) and checks the rows
relative to Postgres's own clock. Exact-date assertions live in the backend's
suite, which injects a clock. A pattern that does not compile is refused when
the file loads.

### Persisted state

`expect.rows` is a query run against the database after the request, and the
rows it must return, in order: a response can be right while the row behind it
is not. Cast every column to text, an integer or a boolean — anything else is
refused rather than compared by how a driver renders it.

### Profiles

A profile is one server configuration, and `scripts/conformance.sh` boots a
server for each. A case runs against its own profile only — a case with none
runs against `default` — so the login cases are never also run against a
server where login is off.

| Profile | Differs from `default` | Port |
|---|---|---|
| `default` | — | `5182` |
| `local-disabled` | `AUTH_LOCAL_ENABLED=false`, `AUTH_GOOGLE_ENABLED=true` (the backend refuses to boot with no provider), `AUTO_MIGRATE=false` | `5186` |
| `founding-disabled` | `FOUNDING_DISABLED=true`, `AUTO_MIGRATE=false` | `5187` |

The other servers share the database and start only once the default one has
migrated it; nothing the `local-disabled` cases do writes a row, and the
`founding-disabled` ones write only what a registration does. Only the
profiles some case uses are waited for.

Adding a profile means a constant in `case.go`, an entry in
`conformance_test.go`'s `servers`, an `*_env` function and a `start` line in
the script, and a §1 port.

### `same_as`: asserting a relation instead of bytes

`expect.same_as` is a second request sent to the **same** server; the case's
answer must equal it in status, every header, and the body bytes. `Date` and
`X-Request-Id` are skipped, since they are minted per response.

It exists for rulings that are relations. #24 says a disabled login route
answers *exactly like an unregistered path* — whatever that answer's bytes
are. `same_as` pins the ruling itself, so a change to the router's 404 cannot
break the case while the relation still holds. A `same_as` identical to the
case's own `request` is rejected at load time, since it would only ever
compare an answer to itself.

**Not generated from the OpenAPI spec, on purpose.** The spec says what a
response *may* look like; a case says what it *is* — which status a specific bad
body produces, what the envelope's `code` is, what the cookie's attributes are.
Generating these would re-derive the ambiguity the harness exists to remove.

### `raw_target`: bytes url.Parse would otherwise normalise away

`request.path` goes through Go's own `http.NewRequest`, which silently fixes
up anything the URL type doesn't like — an unescaped backslash becomes `%5C`,
for instance — before the request ever reaches the server. `request.raw_target`
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
raw socket instead (`backend/internal/httpserver/obs_fold_test.go`), not this
harness.

## Design decisions, and what they rejected

Settled on #28 before any of this was written.

- **A Go test binary, in its own module** (`github.com/kerti/afloat/conformance`),
  not importing the backend. A separate `go.mod` makes that structural rather
  than a rule someone remembers: if this ever imports `backend/internal/...`,
  the thing being tested has leaked into the thing doing the testing. Rejected:
  hurl and schemathesis (another toolchain, and neither asserts persisted
  rows); `bash` + `jq` (no structure to grow into); putting it in the backend's
  suite, where it would silently become a white-box spec.
- **Hand-written expected answers.** Recording the backend's answers and
  diffing against them later would silently bless its bugs.
- **Built and run as a plain process**, not `go run`, which wraps the server in
  a parent that outlives a kill of itself, leaving a port bound and the next
  run failing for a reason unrelated to the code.

## What this does not do

It does not replace the backend's own suite, which tests *how* it works and
runs in seconds. This runs slower, on demand and in CI, and tests only *what
comes out*.
