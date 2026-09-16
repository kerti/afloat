// Package httperr writes the one error envelope both backends emit:
// `{"code": "<CODE>", "args": {...}}`, identical in shape to Balances ADR-0027
// and declared in contract/openapi.yaml.
//
// It carries CODES, NOT MESSAGES. The frontend looks the code up in its i18n
// catalogue and interpolates args; there is deliberately no `message` field,
// because shipping English beside the code invites the two to drift.
package httperr

// Code is the wire-stable identifier. The set here must stay in step with the
// ErrorCode enum in contract/openapi.yaml — the contract leads, and a code
// emitted from here that the contract does not declare is a bug.
type Code string

const (
	// CodeValidation carries {field, rule} in args and reports the FIRST
	// failing field only, matching the contract and the Kotlin side.
	CodeValidation Code = "VALIDATION"

	// CodeInvalidJSONBody is a body that did not decode at all.
	CodeInvalidJSONBody Code = "INVALID_JSON_BODY"

	// CodeInvalidCredentials is deliberately one code for every login failure —
	// unknown email, a User holding no credential, and a wrong password. Splitting
	// them enumerates accounts (BOOTSTRAP.md §5.1).
	CodeInvalidCredentials Code = "INVALID_CREDENTIALS"

	// CodeTooManyAttempts is login backoff, with Retry-After set. Backoff, never
	// a hard lockout: a lockout on a self-hosted household app locks the
	// household out of its own data.
	CodeTooManyAttempts Code = "TOO_MANY_ATTEMPTS"

	// CodeUnauthorized is no valid session.
	CodeUnauthorized Code = "UNAUTHORIZED"

	// CodeCrossSiteRequestBlocked is the second CSRF layer behind SameSite=Lax:
	// a non-safe-method request whose Sec-Fetch-Site or Origin names another
	// site, refused before it reaches a handler.
	CodeCrossSiteRequestBlocked Code = "CROSS_SITE_REQUEST_BLOCKED"

	// CodeInternal is the only code an unexpected failure may produce. The cause
	// is logged; the response says nothing about it.
	CodeInternal Code = "INTERNAL"
)
