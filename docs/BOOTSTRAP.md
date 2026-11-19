# Afloat — Bootstrap Decisions

> **Status:** Ratified 13 September 2026. This is the scaffold sheet: every cross-cutting decision
> that must be true from the first commit, because it is expensive or impossible to change later.
>
> Product scope lives in [`PRD.md`](PRD.md); domain language and arithmetic in
> [`CONTEXT.md`](CONTEXT.md); the *why* in [`VISION.md`](VISION.md). Repo/architecture reasoning is in
> `docs/scratch/Decisions Draft.md`, which this document **corrects** on migrations (§7).
>
> Backend-specific technical choices belong in `docs/adr/go/`, not here. This document is for what
> the backend and the frontend must agree on, and for scaffold-wide conventions.

---

## 1. Identity and naming

Settled first because it gates every identifier in the project.

| Thing | Value |
|---|---|
| Product name | **Afloat** |
| Repository | `github.com/kerti/afloat` |
| Go module | `github.com/kerti/afloat/backend` |
| npm package | `afloat-frontend` (private) |
| Docker image | `afloat` |
| Database | `afloat` |
| Landing page | `afloat.kerti.dev` |
| Application | `app.afloat.kerti.dev` |
| Demo | `demo.afloat.kerti.dev` |
| Preview | `preview.afloat.kerti.dev` |

**Bare unless the name would be ambiguous in its own namespace.** The image and the database are
`afloat`: the frontend is embedded in the backend (§8), so there is one image, and one database. The
Go module is `.../backend` because the repository path is Go's convention. Hyphen where a separator
is needed; never in a Postgres identifier, where it would have to be double-quoted at every use site.

### Local ports

| Service | Port |
|---|---|
| Frontend (Vite dev server) | `5181` |
| Backend | `5182` |
| Postgres | `5184` |
| Postgres, throwaway test container | `5185` |
| Backend, conformance `local-disabled` server | `5186` |

Chosen against three constraints, which matter more than the specific numbers:

1. **Never a service's default.** `8080`, `5173`, `5432`, `3000`, `8000` are what every project
   reaches for first, which is exactly why they collide.
2. **Never `5000` or `7000`.** macOS binds both for AirPlay Receiver, and the resulting failure
   blames your app.
3. **Between 1024 and 32767.** Above that is the ephemeral range — 49152+ on macOS, 32768+ on Linux
   — where an outbound connection can transiently hold the port, so a bind fails at random and does
   not reproduce. Afloat's Postgres was briefly on `55432`: exactly this bug, waiting.

Contiguous because one fact is easier to hold than several, and `5181` sits above Vite's `5173` so
it still reads as the frontend.

**This is Afloat's own allocation and claims nothing about any other project.** No registry, no
reserved block, no assumption that a neighbouring repository has heard of this table. Afloat and
Balances are standalone (`VISION.md` §5); a genuinely shared convention would have to be written down
on both sides to be one at all, and that is not a coupling either app is asking for. Every port here
is overridable — `AFLOAT_DB_PORT`, `AFLOAT_TEST_DB_PORT` — so a collision with something else on the
machine is a variable, not a patch.

**No vanity import path.** Go's convention is the repository path; a vanity path
(`kerti.dev/afloat`) would couple `go get` to the landing page staying up, for no benefit.

**The app gets its own host.** The landing page owns the apex; if both lived there, the SPA fallback
would fight the marketing page for `/`.

**There is an "Afloat Budgeting" on the App Store.** Accepted. It is harmless for a self-hosted
household tool and a portfolio repository, and the name fits the product.

## 2. Repo layout

Per `docs/scratch/Decisions Draft.md` §3, with migrations corrected per §7 below:

```
afloat/
├── backend/                        # Go backend
│   └── internal/
│       ├── migrations/             # CANONICAL goose migrations, hand-written, //go:embed — §7
│       └── auth/common_passwords.txt  # the password denylist — §5.1
├── frontend/                       # React app
├── contract/
│   ├── openapi.yaml                # single source of truth for the API
│   ├── testdata/                   # fixtures the backend's unit tests read — see §6
│   └── conformance/                # black-box HTTP cases against the real backend
├── docs/{adr/go,brand,qa}/
├── scripts/                        # repo tooling the Makefile shells out to
├── docker-compose.yml              # one Postgres, one database
└── Makefile
```

## 3. Stack

Inherited wholesale from Balances-v2 where it exists there, which is most of it.

**Frontend** — React 19, Vite, TypeScript, Tailwind 4, shadcn/Radix, TanStack Query, react-router,
Recharts, i18next, Vitest, Playwright, MSW. Node 22 (`.nvmrc`). Mobile-first; installable PWA with an
app-shell service worker (no push in MVP — see §10).

**Backend** — Go 1.27.x. Migrations embedded via `//go:embed` and applied by the binary, mirroring
Balances.

**Database** — PostgreSQL, one instance, one database (`afloat`).

## 4. Cross-cutting data conventions

- **Primary keys:** UUIDv7, **client-generated**. Column type `uuid`.
- **Idempotent create:** `POST` accepts the client-supplied id; a replay returns **200** with the
  existing resource, a first write returns **201**. Implemented as `INSERT ... ON CONFLICT (id) DO
  NOTHING` plus a read-back.
- **Money:** `DECIMAL(20,4)`, serialised as **strings** on the wire, at exactly the storage scale —
  4 decimal places, always, never trimmed and never exponent notation. `"25000000"` and `"2.5e7"` are
  both wrong for a `25000000.0000` row; `"25000000.0000"` is the only correct rendering. In Go, use
  `decimal.Decimal.StringFixed(4)`, not `.String()`, which strips trailing zeros. Worth an early
  conformance test.
- **Currency:** every monetary value carries its `currency` column even though the MVP UI is pinned to
  the Household's reporting currency.
- **Dates:** `occurred_on` is a `date` (the Period Day). `captured_at` is `timestamptz`. Everything
  else that is an instant is `timestamptz`.
- **Period Day is computed server-side only.** `day_starts_at` plus per-User `time_zone` is real
  arithmetic; if the frontend also derives it, the two disagree at 03:59.
- **Soft delete everywhere:** nullable `deleted_at`. Hard delete is not exposed.
  - *Scope: Household-scoped domain data.* Instance-local auth state — `sessions`, `credentials`,
    `invitations` — is exempt and hard-deletes. Session revocation **is** the row delete; a
    soft-deleted session is a live session that merely looks dead, and every read of the table would
    have to remember the filter. Taking this literally while writing `00001_baseline.sql` produces a
    logout that does not log anyone out.
- **Tenancy:** every query touching Household-scoped data filters `household_id` **in SQL**, not only
  in middleware.
- **Constraints:** `CHECK (amount <> 0)` on expenses — negatives are valid (refunds with no recorded
  original), zero is not.
- **Error envelope:** one shape, following Balances ADR-0027. It carries
  **codes, not messages**; the frontend localises.

## 5. Authentication

**MVP: local email + password only.** Server-side session, session cookie.

Cookie attributes — **host-only** (no `Domain` attribute), `HttpOnly`, `Secure`, `SameSite=Lax`. This
is not a default worth drifting from: `demo.` and `preview.` sit under a shared parent, and a cookie
scoped to `.afloat.kerti.dev` would leak a demo session into preview.

**Session cookie rather than a bearer token, deliberately.** A cookie keeps the entire mechanism
server-side and the client sees one opaque value; revocation is a row delete. A JWT scheme adds claims,
signing and expiry to get right, and nothing in the OpenAPI contract checks any of that.

**Users carry no credential column.** Credentials live in their own table keyed by user. This is the
one design cost paid up front so that Google OAuth later is an additive provider rather than a
migration of the user model — and it is the shape Balances arrived at anyway (a "dormant member" is a
user row with no credential).

**No email in MVP.** Invitations are a one-time link generated and displayed in the UI, delivered by
whatever means the founder likes. Password reset is a CLI command on the instance.

### 5.1 Session and credential mechanics

Settled against Balances' implementation (its ADR-0017, ADR-0027, ADR-0039) so a household running
both apps meets the same login and the same session behaviour in each. Afloat still knows nothing
about Balances at runtime (`VISION.md` §5) — this is shared shape, not an integration. Where Afloat
departs, the reason is below; departures are the interesting part, so don't quietly re-converge.

**Password hashing: Argon2id**, `m=19456` KiB (19 MiB), `t=2`, `p=1`, 16-byte random salt, 32-byte
key. Stored as a PHC string (`$argon2id$v=19$m=...,t=...,p=...$salt$hash`), so the cost parameters
travel with each hash and can be retuned with no migration. These are OWASP's current floor and
Balances' exact values. A hash is verified by the parameters inside it, never by the current
constants — `contract/testdata/argon2.json` pins that against hashes from an independent
implementation as well as Go's own.

**Password policy is a floor only:** minimum length plus a common-password denylist. No composition
rules. Cap the maximum length too, and put a body-size limit on JSON routes — Balances caps only its
file-upload handlers. Both lengths are counted in **Unicode code points**, not bytes and not UTF-16
units: an Indonesian or any non-ASCII passphrase must not be penalised for encoding wider. Go counts
runes.

**The denylist is one file, `backend/internal/auth/common_passwords.txt`**, embedded in the binary.

**Session tokens are hashed at rest.** The cookie carries a 256-bit random, URL-safe value; the
`sessions` primary key is its SHA-256. A database leak yields nothing usable. Hash before every read
and write; the cookie keeps the plaintext, or the next lookup never matches.

**`sessions.user_agent`'s storage rule, in order (#70, on top of #32 item 4's original empty-is-NULL
rule): fold HTAB to SP and trim SP/HTAB from both ends; absent or empty is NULL; not valid UTF-8 is
NULL; otherwise truncated to 512 Unicode code points, on a code point boundary.** The backend may
not read a client-supplied header and hand it to the database unexamined: Go's `r.UserAgent()`
carries a header's raw bytes through untouched, and a Postgres `TEXT` column is UTF8-encoded, so an
invalid header used to fail the whole login with a 500 (`invalid byte sequence for encoding "UTF8":
0x85`) that had nothing to do with the password. The fold step exists because servers and proxies
unfold an obsolete header fold (RFC 7230 §3.2.4's obs-fold) differently: Go collapses
`CRLF 1*(SP/HTAB)` to a single SP; others drop only the CRLF and keep the fold's own whitespace, tab
included. Folding HTAB to SP and trimming both ends converges every measured case (an internal tab, a
fold with nothing before it, a fold with nothing after it) to the same stored string whichever hop did
the unfolding — a real inline tab a client meant literally is stored as a space, because past this
rewrite there is no byte left to tell the two apart by. `sanitizeUserAgent` applies the whole rule at
capture time, before the value ever reaches a session row; a raw-socket test (`obs_fold_test.go`)
exercises it end to end, since a client library that refuses to put a raw CR or LF in a header value —
as Go's own does — cannot drive the conformance harness through it.

**Sliding TTL, with an absolute cap — this is a departure.** Balances refreshes `expires_at` on every
authenticated request and never consults `created_at`, so a stolen cookie stays valid for as long as
the attacker keeps using it and the 30 days never arrive. Afloat keeps the sliding window and adds an
absolute lifetime checked against `created_at`. Re-authenticating a few times a year is not friction
worth a permanent session for.

**Touch the session on a threshold, not on every request — also a departure.** Balances issues an
`UPDATE` per authenticated request, which makes every `GET` a write on the one table read by every
request. Only refresh when more than half the TTL has elapsed: same observable behaviour, a fraction
of the writes, no row bloat. Capture is the hot path (PRD Q-11) and the PWA re-probes on every resume.

**Login must not enumerate accounts.** Unknown email, a User with no credential, and a wrong password
all return one `INVALID_CREDENTIALS`; the comparison is constant-time; and a request for an
address with no account still pays the full hashing cost, so timing cannot distinguish present from
absent either. All three, not two of three.

**A stored hash's cost is capped too: `m ≤ 65536` KiB and `t ≤ 10` (#68).** The PHC string carries its
own cost (above), which is what lets the current constants be retuned with no migration — but read
back unbounded, that same freedom lets a stored string alone dictate how much memory and time one
verify spends, worse once the concurrency cap below admits several such verifies at once. `65536` KiB
(64 MiB) and `10` iterations are headroom over the operating cost (`m=19456`, `t=2`) — about 3.4× the
memory and 5× the iterations — not x/crypto's absence of one. `parsePHC` refuses a stored hash above
either bound, through the same silent-log reject path as every other malformed PHC string (below). A
cost increase past either ceiling means raising the ceiling first, deliberately, rather than a stored
string quietly being allowed to cost more than the code that reads it expects.
`contract/testdata/argon2.json` carries accept rows at exactly `m=65536` and `t=10` and reject rows one
past each; the suite reads them.

**Concurrent Argon2id hashing is capped at 4, process-wide — a departure surfaced by
#33.** `m=19456` KiB is allocated *per hash in flight*, not once, so with no bound an unauthenticated
caller chooses the process's peak memory: 30 concurrent logins want `30 × 19 MiB ≈ 570 MiB`, and a Go
process that exceeds available memory is killed outright rather than degrading. Afloat's deployment target is a self-hosted box
(`VISION.md`), where that is the whole machine. At the cap, `4 × 19 MiB ≈ 76 MiB` of Argon2 memory
live at once — worst case `4 × 64 MiB = 256 MiB` if every hash in flight happened to be a stored hash
at the #68 ceiling. That is live memory, not the process's footprint: a finished hash's memory is reclaimed
only when the garbage collector runs, and Go's default `GOGC=100` lets the heap reach about twice its
live size first, so the peak can run to roughly double. Still bounded, which is the point. Retuning
Argon2id's cost parameter later must be checked against this arithmetic before it ships, or the memory
ceiling moves silently. The cap is a **fixed constant, not a §12 environment
variable** — small enough to fit the smallest supported deployment, large enough (~80 hashes/sec at
this cost, on a host with at least four cores) that a household's simultaneous logins are never
serialised behind each other in practice.

The dummy-cost-equalizer hash (the unknown-address path above) shares the **same** semaphore as a
real hash, not a separate one — the bound is on total concurrent Argon2 work regardless of account
validity, or bounding only the real path leaves the dummy path free to blow the same ceiling. A
request beyond the cap **queues for a permit rather than answering 429 immediately**: an immediate
429 would leak that the server is busy, which is not an account-level fact, and the login path's
whole constant-work design (previous paragraph) is built to hide exactly that kind of signal. Queueing
adds latency that is a function of load, not of the account being checked, so the enumeration-
resistance property survives it. The wait is bounded by the timeout the request already has, not a
second one invented for this step: the request context's deadline, set by chi's `Timeout`
middleware from `HTTP_WRITE_TIMEOUT`. A login still queued when the wait runs out answers `500 INTERNAL` and records **no** backoff failure, because its password was never checked.

**The deadline bounds the wait, not the login.** A login that took its permit in time finishes: the
second backoff read, the hash, the failure write and, on success, the session. That work runs on a
context detached from the request, bounded by the handler timeout again, counted from the permit. So
a login can run longer than `HTTP_WRITE_TIMEOUT` end to end, and its answer can then miss the connection's write deadline (`HTTP_WRITE_TIMEOUT` plus the 5s grace, §12),
but its failure is still recorded, and that is the part that matters.
Bounding it by the request deadline instead lets a flood switch the backoff off: behind a queue
filled faster than the cap clears, the login that gets a permit is the one about to run out of time,
its hash carries it past the deadline, and the failure write then fails. Its guess was checked and
answered `401`, but nothing was recorded, so the backoff never grows. Measured before the fix,
with the deadline scaled down to 300 ms: under a flood of unknown addresses, 4 to 9 guesses at one
account were answered `401` against a recorded `failure_count` of 1. A client that leaves after its
permit is taken no longer stops the login either; the answer is lost, but the failure is recorded.

**A login reads its backoff twice, the second time holding its permit.** The first read, before the
queue, answers a throttled caller without making it wait. It cannot be the only one: every login in a
burst on one account reads it before any of them has failed, so they all pass it, all queue, and all
have their passwords checked, at the cap's ~80 guesses a second for as long as the burst lasts, the
backoff never applying. So the permit is held from a second read, through the hash, to the failure
write. One caller's failure is written before the permit passes on, and the next caller reads it and
answers `429` without hashing. At most the cap's worth of logins hold permits at once, so a burst on one
account gets up to 4 guesses before the backoff applies, not the whole queue. The `429` is the same
answer the first read gives, keyed on the address and the IP whether or not the account exists, so the
wait before it tells a caller nothing the first read would not. The permit covers database round
trips as well as the hash: the backoff read and, on a failure, one write per key (the address's and the
IP's). So the cap's ~80 logins a second assumes a quick database. A slow one holds each permit longer
and lowers that rate for as long as it stays slow; nothing breaks, and it recovers when the database
does.

The queue moves what a login flood costs; it does not remove it. What runs ahead of the queue is not
capped: the first backoff read and the two credential lookups share the database pool with every
endpoint, so a flood fast enough to saturate the pool slows everything, as it did before the cap. A
queued login is a parked goroutine, so the queue itself slows logins and no other endpoint. But nothing bounds how many wait: each holds its goroutine and client connection, a few
tens of KiB rather than a hash's 19 MiB, for up to `HTTP_WRITE_TIMEOUT`, so a flood faster than the cap
clears (about 80 logins a second) grows memory with its rate for as long as it lasts. It holds no
database connection while it waits: pgx takes one per query. A client that leaves
ends its wait, and no hash runs for it. Whether the wait needs a bound is open: #54.

**The rate-limit key is the connection's own address, never `X-Forwarded-For`.** Self-hosting means
there may be no proxy in front, so nothing strips that header and it is attacker-controlled — using
it means an attacker picks a fresh key per request and the per-IP backoff stops existing. chi's
`RealIP` middleware stays unmounted. A deployment that does
put a trusted proxy in front changes this behind a config flag, never by default.

**Login backoff lives in Postgres — a departure, and the load-bearing one.** Per-IP and per-email
exponential backoff, capped, returning `429` with `Retry-After`. Backoff, never a hard lockout: a
lockout on a self-hosted household app is a footgun. Balances keeps the limiter in process memory. A
table keyed by ip/email with a `backoff_until` is testable, shared by every process serving the
instance, and survives a restart, which the in-memory version does not.

The curve is `LOGIN_FIRST_BACKOFF` (default `1s`) after the first failure, doubling per failure,
capped at `LOGIN_MAX_BACKOFF` (default `5m`) — §12. The doubling stops once it passes the cap, however
many failures follow: unclamped, it overflowed Postgres's interval at the 45th, and from then on the key was
never throttled again. What those defaults produce, and which keys a
failure writes, is `contract/testdata/login_backoff.json`; the suite reads it, and holds the defaults
to it (#16). The same goes for Argon2id: `contract/testdata/argon2.json` carries hashes minted by two
independent implementations, which must verify, and strings that must not. The accepted spelling is
exactly `$argon2id$v=19$m=…,t=…,p=…$salt$hash`, unpadded standard base64, `8p ≤ m ≤ 65536` KiB,
`1 ≤ t ≤ 10` (#68's ceiling, above), `1 ≤ p ≤ 255`, a hash of at least 4 bytes, and no base64 segment
of a length that cannot decode: the parser was once looser than that, and a malformed stored string
answers "does not verify", never an error or a panic.

**A database outage is not a statement about an account (#23, #27).** Only a lookup that genuinely
found nothing counts as an absent user, credential or session: `pgx.ErrNoRows`. Every other failure — a dropped connection, an exhausted pool — answers login
with `500 INTERNAL`, not `401 INVALID_CREDENTIALS`, and records **no** backoff failure. Session
resolution **leaves the cookie alone** and lets the request continue unauthenticated, so `/me` answers
`401` and the same cookie works again once the database is back. Answering an outage as though it were
about the account costs something that outlives the outage: a password reset that was never needed,
every address that tried to log in during it left sitting behind a backoff window, every active
session logged out. A 500 is not an answer about any one address: an outage that takes the database
down fails the lookups for every address alike. It is not quite uniform, though. A known address makes
one more query before the Argon2 permit than an unknown one (the credential lookup), so under a starved
pool its 500s come a little more often, and a caller flooding logins could compare the rates. That
residual, and the extra round trip it rides on, is #55.

**Any instant the database also evaluates comes from the database.** `aa1f68f` moved
`activeBackoffSeconds`, `findLive` and `touch` onto the database's `now()` rather than the app's,
because a window the database evaluates (a session's absolute lifetime, an active backoff) must be
measured by the database's clock — subtracting against the app's own clock in a second step lets
app/DB skew silently lengthen, shorten, or defeat the check, and the two disagree exactly at the
boundary a caller cares about. This bit once after that commit: the login backoff computed the
remaining wait by subtracting `h.now()` from a timestamp the database had already filtered on its own
`now()` (fixed in #25 by having the query return the remaining interval, computed in SQL, instead of
a raw timestamp). Likewise a session's `created_at`/`last_seen_at` are left to the schema's
`DEFAULT now()`, because the absolute-lifetime check compares them against the database's `now()`.
When a boundary is checked in SQL, compute
anything derived from that boundary in the same SQL, and never re-derive it in application code
against a different clock.

**Revoke every session on a password reset.** The reset is the "assume it was compromised" lever;
delete the user's sessions inside the same transaction before minting the new one.

**A second CSRF layer behind `SameSite=Lax`.** Reject a non-safe-method request whose
`Sec-Fetch-Site` or `Origin` names another site, before it reaches a handler —
`CROSS_SITE_REQUEST_BLOCKED`, 403.

**`Secure` on the session cookie is configurable only so local dev over HTTP works.** It defaults to
true; no deployment document suggests otherwise.

### 5.2 Wire shape

`/auth/local/*` is namespaced from day one so Google arrives as `/auth/google/*` without moving the
password routes. Login returns **204** with the cookie and no body; the client then calls `GET /me`.
That looks like a wasted round trip and isn't: Google arrives as a redirect that cannot return a body,
so the client needs the fetch-after-auth path regardless, and one post-auth path beats two.

**The session cookie is `afloat_session`, not Balances' bare `session`** — the one place sameness is
actively harmful. Host-only cookies don't collide across hostnames, but a self-hoster putting both
apps behind one hostname on different paths would have them silently overwrite each other.

The error envelope is Balances ADR-0027 exactly: `{"code": "SCREAMING_SNAKE", "args": {...}}`, no
`message` field, `args` values JSON primitives only. `VALIDATION` carries `{field, rule}` and reports
the first failing field only.

**Which request-body failure gets reported (#18).** The login tests drive this table:

- A body that is not an instance of the declared shape is `INVALID_JSON_BODY` with no `args`, whatever
  else is wrong with it. That covers malformed JSON (anything after the value, bytes that are not
  strict UTF-8, a byte order mark and a UTF-16 or UTF-32 body included), a body not sent as
  `application/json` (another `+json` type included), a non-object root, a wrong type (no scalar
  coercion: `5` is not a string), a `null`, and a property the schema does not declare.
- The media type is matched case-insensitively (ASCII letters only), whitespace around it ignored
  (RFC 9110 §8.3.1). A body is read as UTF-8 whatever `charset` its `Content-Type` claims, a name
  no decoder knows included: `application/json` has no such parameter (RFC 8259 §11). Every other
  parameter is ignored too, an unparseable one included.
- Otherwise every failing field competes, an absent required one (`rule: required`) included. The
  reported one is the least by wire field name, then by rule.
- `rule` uses go-playground/validator's tag names (`required`, `min`, `max`, `email`), plus `pattern`.
- `minLength` and `maxLength` count code points, JSON Schema's characters: an emoji is one, not two
  UTF-16 units or four bytes.
- `format: email` trims, then validates: a padded address is accepted and trimmed again for the
  lookup, and an empty or all-space one is not an address. The trim is inside the format check only,
  so `maxLength` counts the padding. "Trim" is Go's `strings.TrimSpace`, and the address rule is the
  WHATWG one for `<input type=email>`, so the server accepts what a browser's email field does.
  `contract/testdata/email.json` lists the answers it must give.

This comes from the spec-validating middleware (`httpserver/openapi_validate.go`). The generated
models carry `format: email` as a plain `string` (`oapi-codegen.yaml`), so the decode does not check
the address a second time, and it lowercases the media type, which kin-openapi compares byte for
byte.

**Paths and the body cap (#56).** Three rules, each a case in `contract/conformance/cases/routing.yaml`:

- **A percent-encoded path is its decoded route** (RFC 3986 §6.2.2.2): `/api/auth/local/%6Cogin` is
  login. `routeOnDecodedPath` clears `RawPath` so chi routes on the decoded path. **An encoded slash
  never becomes a separator:** the raw path is kept for chi when it holds `%2F`.
- **A path with no route is a path that does not exist.** `;`, `//`, a dot segment, an encoded slash,
  backslash or percent, `%00`, invalid UTF-8, an overlong or raw non-ASCII byte sequence (#64), and a
  non-OPTIONS method with the asterisk-form target `*` (RFC 9110 §7.1 reserves that form for OPTIONS):
  each gets the router's bare unregistered-path 404, headers included. No `ErrorCode` for it. The
  disabled-login gate compares the decoded path, so no spelling of the login path escapes it. An
  unknown method (`FOO`, `PROPFIND`) answers 405 — chi's own method-not-allowed handling, on every path
  including an unregistered one (#71).
- **The request line and headers are capped at 8 KiB (#92)**, `MaxHeaderBytes` in `cmd/afloat/main.go`,
  not `net/http`'s 1 MiB default: self-hosting means there may be no proxy in front to cap them. A
  fixed constant, not a §12 variable. `net/http` allows about 4 KiB of slack on top, then answers
  `431` itself, before any handler runs — so no `ErrorCode` for it.
- **Header values are unrestricted (#67)**, a UTF-8 byte in the 0x80–0xFF range included. Header
  *names* stay strict: `net/http` refuses an invalid name outright — verified with `X-\xe9: 1` and a
  DEL or HTAB byte in the name, each a bare `400 Bad Request` — and refuses a genuine control character
  in a header *value* (C0 minus HTAB, plus DEL) the same way, before `httpserver` ever sees the request.
- **HEAD and OPTIONS (#66).** HEAD answers like GET — RFC 9110 §9.1 requires it of a general-purpose
  server, and a `headAsGet` middleware ahead of the contract's own router provides it (chi's
  `middleware.GetHead` rewrites only its routing lookup, not `r.Method`, which the spec-validating
  middleware still reads). OPTIONS is a flat, content-free 405 everywhere — no `Allow` header, on a
  registered route, an unregistered path, the disabled-login route, or a malformed path alike — because
  Afloat is same-origin with no CORS, so no client sends it, and an `Allow` header would otherwise tell
  a prober which routes exist; answering it identically everywhere is also what keeps the disabled-login
  route indistinguishable from an unregistered path on this method, with no second gate to keep in sync
  with the first. `OPTIONS *` (RFC 9110 §7.1's asterisk-form) gets the same 405: `net/http`'s own
  built-in 200 for it is switched off via `Server.DisableGeneralOptionsHandler`. `OPTIONS *?x=1` is
  malformed enough that Go's own request-line parser refuses it outright with a bare 400, before any
  handler runs.
- **The 1 MiB cap meets only the bytes a handler reads**, as Go's `MaxBytesReader` does: a declared
  `Content-Length` is not refused up front, and `/health` or logout answers normally with any body.
  The spec validator runs with its copy of the spec stripped of security requirements, because
  kin-openapi reads the whole body into memory before calling even a no-op `AuthenticationFunc`,
  and under the cap that read made an oversize body on logout or `/me` a 401.

### Response headers

The backend sends this fixed set on every response, as explicit middleware (issue #26):

| Header | Value |
|---|---|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `Cache-Control` | `no-cache, no-store, max-age=0, must-revalidate` |
| `Pragma` | `no-cache` |
| `Expires` | `0` |
| `X-XSS-Protection` | `0` |
| `X-Request-Id` | The inbound id, sanitised; otherwise one minted server-side (below). |

**`X-Request-Id`** is honoured inbound so a reverse proxy's id survives into the backend's logs,
but the header is attacker-controlled when nothing sits in front, so it is sanitised first: keep only
ASCII `A–Z`, `a–z`, `0–9`, `-` and `_` (every other byte dropped, not replaced), then cut to 64. If
nothing survives, or none was sent, mint one: 8 random bytes as 16 lowercase hex characters. The
backend implements this itself rather than using chi's `middleware.RequestID`, which echoes unfiltered and
mints ids that carry the hostname and a request counter.

**The backend does not send `Strict-Transport-Security`.** HSTS is policy about the operator's domain
(`max-age`, `includeSubDomains`), not about Afloat, and whatever terminates TLS owns it — the reverse
proxy in any deployment this document supports.

## 6. The API contract

`contract/openapi.yaml` is **hand-written and authoritative**; every artefact is generated from it.

| Surface | Generator |
|---|---|
| Go | `oapi-codegen` — server interfaces and types |
| React | `openapi-typescript` |

`make check` regenerates into a scratch directory and diffs (`scripts/check-generated.sh`), the
equivalent of Balances' `backend-gen-ts-types-check`.

**The base path is the spec's `servers[0].url` (#17).** oapi-codegen emits nothing for it, so the
backend reads it from the embedded spec at startup (`httpserver/basepath.go`) and refuses to boot on
an entry it cannot mount at.

> **This is contract-first; Balances is code-first.** Balances generates TypeScript *from Go*
> (`backend/tools/gen-routes`, `gen-ts-types`). Afloat's spec leads instead, so the frontend's types
> and the backend's interfaces come from the same hand-written file.

**Trajectory is computed server-side.** The client formats and floors for display but derives
nothing. This keeps one source of truth.

**`contract/testdata/trajectory.json` is a test fixture** holding the reference rows from
`CONTEXT.md` §Worked check. The backend's unit tests read it. The conformance harness proves the
backend serves the contract's *shape*; only this proves it computes the right *numbers*.

## 7. Migrations — correction to the Decisions draft

The Decisions draft's plan of a top-level `db/migrations/` is replaced: the
migrations are **goose files, canonical and hand-written, in `backend/internal/migrations/`**, where
the binary embeds them (`//go:embed` cannot reach a parent directory, so they live in the package that
embeds them). Each file carries `-- +goose Up` and `-- +goose Down` together, as Balances' do.

| Path | Role |
|---|---|
| `backend/internal/migrations/00001_name.sql` | **Canonical.** Up and Down in one file. `//go:embed`ed and applied on boot. |

- sqlc reads the same files as its schema (their Up sections), so there is one schema source.
- `make test-migrations` (`scripts/test-migrations.sh`) applies the migrations **through goose** to a
  throwaway Postgres and asserts the schema behaves — defaults, every `CHECK`, the soft-delete-aware
  indexes, cascade behaviour, the §7.1 in-place-edit hazard, and that Down drops what Up created.
  Through goose rather than `psql`, because `psql` would run each file's Up *and* Down, and because the
  runner is what production uses. It needs docker, which is why `check` does not gate on it; CI runs it.
- `make gen` runs every generator; `make check` calls `scripts/check-generated.sh`, which regenerates
  into a scratch directory and compares rather than regenerating in place and running `git diff`.
  That obvious version is wrong twice: it repairs a hand-edit instead of reporting it, and `git diff`
  says nothing about files git does not yet track. Both were real bugs here.
- The binary self-migrates on boot, mirroring Balances.

### 7.1 Migrations are editable in place until the first non-resettable database

Until then, a schema change **edits `00001_baseline.sql` in place**; it does not add `00002`.
Pre-release, a tidy baseline is worth more than an accurate archaeology of how it got that way, and
nothing downstream has a history to preserve yet.

**The trigger is a database you cannot drop — not a version number.** Balances reached for `v1.0.0`
first and then corrected it (its ADR-0033, amended 2026-07-02): immutability begins at the first
deployment to something non-resettable, *whatever* version that happens to carry, and squashing stays
permitted for migrations that only ever ran in resettable environments. The number was only ever a
proxy for the thing that matters.

For Afloat the binding case is not a tag at all. `demo.` and `preview.` stay resettable as long as
they actually reset. **The Household that starts using Afloat for real is the non-resettable one**,
and that will happen months before anything is tagged. From that day, `00001` is frozen and every
change is a new file, whatever the version string says.

**While the freedom lasts, an in-place edit is only half a step — the other half is dropping the
database.** goose does not fail at all on an edited migration: it tracks version numbers and nothing
else, so it sees version 1 already applied, skips the file, and leaves the database on the **old**
schema while the code expects the new one. Never edit in place without resetting the database —
`make db-reset` is that command, and is what makes this policy safe to follow rather than a trap.

**Observed, not reasoned about.** `scripts/test-migrations.sh` appends a column to a scratch copy of
`00001`'s Up section and re-runs goose against a database already holding version 1: goose reports
`no migrations to run` and the column is absent afterwards. `goose_db_version` has only `version_id`,
`is_applied` and `tstamp` — no checksum. The test asserts it, so if goose ever changes behaviour, this
section fails rather than quietly becoming wrong.

## 8. Frontend delivery

The backend embeds `frontend/dist` via `//go:embed`, with SPA fallback: unknown path → `index.html`,
but never for `/api/*` and never for the service worker file, which must be served at root scope with
correct cache headers or the PWA silently stops updating.

## 9. Internationalisation

**en-GB and id-ID from day one**, both locale files real. i18next, matching Balances.

Beyond the mechanical argument, this is a product gate: *Afloat / Drifting / Taking on water* is an
English nautical idiom, and whether it survives translation into Indonesian is something to learn
**before** the brand session builds an identity around a waterline, not after. See `PRD.md` Q-10.

Backend returns error codes; the frontend owns all user-facing strings.

## 10. Planned and committed — not in MVP, but not speculative

These are design constraints, not wishes. Each is named here so nothing is built that forecloses it.

1. **Google OAuth.** A second auth provider in the backend. Enabled by the separate
   credentials table (§5). No user-model migration required.
2. **Email.** Invitations and password reset by email, replacing the on-screen link and the CLI reset.
   Additive; the invitation token model should be built as though email will deliver it.
3. **API keys.** Scoped, rotatable, per-Household, for the Balances read path. A third authentication
   mechanism. Read endpoints exist in MVP and are session-authenticated; nothing about them should
   assume a human session.
4. **Import.** Spreadsheet import with **app-provided templates**, mirroring Balances' flow. The
   vocabulary is reserved now so nothing else claims it: **Import** = template-based spreadsheet
   ingest into a live Household; **Export** = the spreadsheet download; **Backup / Restore** =
   whole-Household, and separate.

**Not in MVP and not planned:** telemetry of any kind. G3 (open frequency) is derived from
`captured_at`; G2 is timed by hand. This deletes an events table, an endpoint and a privacy
conversation from a self-hosted app.

> **Known consequence of starting fresh.** With no import at launch, the first Monthly Budget has no
> historical anchor. Partial mitigation is already in the model: onboarding asks for
> `expected_monthly_income` and proposes a Budget as a share of it (`PRD.md` S5). The budget will be
> less well-grounded than it would be with three months of real spend behind it — accepted knowingly.

## 11. Scaffold order

1. Repo skeleton, `LICENSE` (AGPL-3.0), `README.md`, `.nvmrc`,
   `Makefile` shell, `.githooks`.
2. `contract/openapi.yaml` — health check plus auth only. Enough to prove the generator chain.
3. The baseline migration — households, users, credentials, sessions, invitations.
4. `docker-compose.yml`; Postgres up; migrations green.
5. Backend: module init, config, DB, embedded migrations, generated server interfaces, health,
   auth, session cookie.
6. *Retired.*
7. Frontend: Vite scaffold, i18next with both locales, TanStack Query, generated types, router, auth
   screens, PWA manifest and app-shell service worker.
8. `contract/testdata/trajectory.json` and the calculation unit tests, **before the dashboard
   endpoints exist**. Red tests first.
9. Domain: households, pockets, categories, budgets, expenses. Then trajectory. Then the dashboard.
10. Caddyfile, deploy.

## 12. Environment variables

The table below is what an operator configures from. `scripts/check-env-docs.sh` — wired into
`make check` — treats it as the source of truth and fails if the backend's configuration drifts from
it: a name read but not documented, documented but not read, or a default that differs.

### Backend configuration

| Variable | Default | Notes |
|---|---|---|
| `DATABASE_URL` | *(required)* | libpq form. Blank or absent fails at boot, never at first request. |
| `PORT` | `5182` | §1. |
| `LOG_FORMAT` | `text` | `text` for a terminal, `json` where logs are collected. |
| `LOG_LEVEL` | `info` | |
| `AUTO_MIGRATE` | `true` | Apply migrations on boot. Off only to run against a database migrated by something else. |
| `HTTP_READ_TIMEOUT` | `30s` | |
| `HTTP_WRITE_TIMEOUT` | `60s` | The handler-timeout middleware (`middleware.Timeout`, issue #30) uses the value itself; `http.Server.WriteTimeout` is the value plus a fixed 5s grace, because with the two equal the connection's write deadline passes first and the cut-off handler's 503 never reaches the client. The same deadline bounds a login's wait for an Argon2 permit (§5.1, #33). Must be positive: the backend refuses to boot on `0` or a negative value, since `0` does not mean "no timeout" to the handler timeout but a deadline already passed. |
| `HTTP_IDLE_TIMEOUT` | `120s` | |
| `SHUTDOWN_TIMEOUT` | `10s` | |
| `AUTH_LOCAL_ENABLED` | `true` | |
| `AUTH_GOOGLE_ENABLED` | `false` | Planned, not built (PRD §4.1). |
| `SESSION_TTL` | `720h` | The sliding window (§5.1). |
| `SESSION_MAX_LIFETIME` | `2160h` | The absolute cap from `created_at` (§5.1). |
| `LOGIN_FIRST_BACKOFF` | `1s` | The login backoff after the first failure; doubles per failure (§5.1). Must be at least 1ms (#69) — a boundary `validate()` names in its startup error. |
| `LOGIN_MAX_BACKOFF` | `5m` | The backoff's cap. Not shorter than `LOGIN_FIRST_BACKOFF`. The backend refuses to boot on either violation. |
| `COOKIE_SECURE` | `true` | Off only for local development over plain HTTP. |
| `VERSION` | `dev` | Stamped at build time; reported by `GET /api/health`. |

`AFLOAT_REQUIRE_TEST_DB` is test-only and so not in the table: set to `1`, it turns the integration
suite's docker-missing *skip* into a *failure*. CI sets it once.

### Parsing rules

**A set-but-empty duration variable is treated as unset, for every duration variable (#69).** `FOO=`
in a `.env` file boots with `FOO`'s documented default, not a parse failure. "Empty" is exactly the
empty string, never whitespace-only (#69's second review): `LOGIN_FIRST_BACKOFF="   "` is refused, the
same as any other unparseable spelling, because `caarlos0/env` only special-cases `value == ""`
(`getOr`'s `exists && value == "" && defExists` case, true for any field carrying an `envDefault` tag,
duration or not).

**Durations never use `d`.** Go's `time.ParseDuration` understands `ns us ms s m h` and rejects `d`.
The values above are spelled in hours for this reason, and tidying `2160h` into `90d` would crash the
backend at boot. `check-env-docs.sh` rejects a `d` suffix on any duration variable.

### Compose-level variables

`docker-compose.yml` reads `AFLOAT_DB_PORT`, `AFLOAT_DB_USER` and `AFLOAT_DB_PASSWORD` to build the
local Postgres. They configure the *container*, not the backend, and are out of the table above — the
backend never reads them. `.env.example` carries both groups, separated.
