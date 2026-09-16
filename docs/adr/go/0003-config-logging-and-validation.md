# Config, logging and validation

`draft`

Three small picks, recorded together because none justifies its own file and all three are load-
bearing from the first commit: **`caarlos0/env/v11`** for configuration, **`log/slog`** for logging,
**`go-playground/validator/v10`** for request validation.

## Configuration: environment only, parsed into a struct, validated at boot

`caarlos0/env` maps environment variables onto a tagged struct. No config file, no flags beyond the
subcommand.

- **Self-hosting is the deployment model**, so configuration arrives as environment variables in a
  compose file. A config-file format would be a second thing to document and keep in sync.
- **Missing required configuration fails at boot**, not at the first request that needs it. The
  struct is parsed once in `main`, and a server with no `DATABASE_URL` never reaches `ListenAndServe`.
- Defaults live in struct tags beside the field, so the default and its documentation cannot drift.

Afloat's ports come from `BOOTSTRAP.md` §1 — the Go backend defaults to `5182`, and the default is
written in the tag rather than assumed.

## Logging: `log/slog`, structured, no dependency

Standard library since 1.21. Structured output, levels, handlers, and nothing to choose.

**No request logging of user content, ever.** `PRD.md` N6 forbids telemetry of any kind, and a log
line containing an Expense description is telemetry that happens to be written to disk. Logs carry
request method, path, status, duration and error codes — never bodies, never amounts, never the
session token. Error paths log the internal cause; the response carries only a code
(`BOOTSTRAP.md` §4).

## Validation: `go-playground/validator/v10`

Struct-tag validation on decoded request bodies, producing the `VALIDATION` code with `{field, rule}`
args that `contract/openapi.yaml` already declares.

The contract is the primary source of what is valid, and `oapi-codegen` enforces the parts OpenAPI
can express — required fields, types, enums, `maxLength`. `validator` covers what it cannot, and
handlers stay free of hand-written `if x == "" ` chains that drift from the spec.

**Where the two disagree, the contract wins** and the struct tag is the bug.

## Consequences

- `backend/internal/config` owns the `Config` struct and is the only package that reads the
  environment. Nothing else calls `os.Getenv`.
- The `slog` handler is selected by `LOG_FORMAT` (`text` for dev, `json` for deployment) and
  installed as the default logger in `main` before anything else runs.
- Validation failures map to `VALIDATION` in `internal/httperr`, first failing field only, matching
  the contract and the Kotlin side.
- **Handlers use `httperr.Validator()`, never `validator.New()`.** The shared instance registers a
  tag-name function so a failure reports the JSON field name; without it `DisplayName` is reported as
  `displayname` and the frontend looks up a catalogue key that does not exist. A single-word field
  like `email` hides the bug, which is how it survived its first test.
