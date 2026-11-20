package auth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/httperr"
)

// timeZoneRule is go-playground/validator's tag for an IANA zone, and the rule
// reported when one is not (BOOTSTRAP.md §4's vocabulary is the validator's).
// It is time.LoadLocation plus a refusal of "" and "local": the host's zoneinfo
// first, then the copy cmd/afloat embeds (time/tzdata) as a floor.
// contract/testdata/time_zones.json pins the rows that do not depend on the
// host.
const timeZoneRule = "timezone"

// UpdateMe changes the caller's own settings: their name, language and time
// zone. Merge semantics, one statement, last write wins per field (BOOTSTRAP.md
// §4): a field left out is not touched, and two writers of different fields
// both land. The email and the password are not here, because both need the
// current password (change_email.go, change_password.go).
//
// The User is the authenticated one, taken from the context and never from the
// request, and the UPDATE filters on both of its ids in SQL.
func (h *Handlers) UpdateMe(ctx context.Context, request api.UpdateMeRequestObject) (api.UpdateMeResponseObject, error) {
	user, ok := UserFromContext(ctx)
	if !ok {
		// Unreachable through the router: httpserver's sessionRequired answers
		// 401 for this operation first. Kept so a router change that lost the
		// check degrades to the contract's 401, not an UPDATE with a zero id.
		return api.UpdateMe401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}
	body := request.Body
	if body == nil {
		// Unreachable: the contract's body is required, and the validator
		// refuses an absent one first.
		return api.UpdateMe400JSONResponse{
			BadRequestJSONResponse: api.BadRequestJSONResponse{Code: api.INVALIDJSONBODY},
		}, nil
	}

	params := db.UpdateUserParams{UserID: user.ID, HouseholdID: user.HouseholdID, TimeZone: body.TimeZone}
	if body.Locale != nil {
		locale := string(*body.Locale)
		params.Locale = &locale
	}

	// The rules the contract cannot state. Every one that fails is collected
	// and the least (field, rule) reported, as the spec validator does for the
	// ones it can state. They are reached only once every contract rule has
	// passed: the validator answers first (BOOTSTRAP.md §5.1.1).
	var failures []fieldFailure
	if body.UserDisplayName != nil {
		name, rule := CheckUserDisplayName(*body.UserDisplayName)
		if rule != "" {
			failures = append(failures, fieldFailure{"user_display_name", rule})
		}
		params.UserDisplayName = &name
	}
	if body.TimeZone != nil {
		if err := httperr.Validator().Var(*body.TimeZone, timeZoneRule); err != nil {
			failures = append(failures, fieldFailure{"time_zone", timeZoneRule})
		}
	}
	if first, failed := leastFailure(failures); failed {
		return api.UpdateMe400JSONResponse{
			BadRequestJSONResponse: api.BadRequestJSONResponse{
				Code: api.VALIDATION,
				Args: &map[string]interface{}{"field": first.field, "rule": first.rule},
			},
		}, nil
	}

	// The answer is built from the row the UPDATE returned, not from the User in
	// the context, which was read before the write.
	row, err := h.q.UpdateUser(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// A User who is gone cannot be served coherently, as in GetMe.
			slog.Error("update me: user missing", "user_id", user.ID)
		} else {
			// Nothing here logs a request body: a name is user content.
			logQueryError("update me", err)
		}
		return internalError[api.UpdateMeResponseObject]()
	}
	household, err := h.q.GetHouseholdByID(ctx, row.HouseholdID)
	if err != nil {
		logQueryError("update me: look up household", err)
		return internalError[api.UpdateMeResponseObject]()
	}
	return api.UpdateMe200JSONResponse(meResponse(row, household)), nil
}

// fieldFailure is a field breaking a rule the contract cannot state.
type fieldFailure struct{ field, rule string }

// leastFailure is the failure to report: the least by wire field name, then
// rule, whichever check found it first.
func leastFailure(failures []fieldFailure) (fieldFailure, bool) {
	if len(failures) == 0 {
		return fieldFailure{}, false
	}
	first := failures[0]
	for _, f := range failures[1:] {
		if f.field < first.field || (f.field == first.field && f.rule < first.rule) {
			first = f
		}
	}
	return first, true
}
