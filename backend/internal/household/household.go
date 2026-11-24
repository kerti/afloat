// Package household owns the Household's settings (PRD S3): today, the one
// write, PATCH /household. Handlers live with their domain (docs/adr/go/0001).
//
// The dependency runs one way: this package imports auth, for the User in the
// request context and for the Household's wire projection, and auth never
// imports this one.
package household

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/httperr"
)

// Handlers implements the Household half of api.StrictServerInterface.
type Handlers struct {
	q db.Querier
}

type Deps struct {
	Querier db.Querier
}

func New(d Deps) *Handlers {
	return &Handlers{q: d.Querier}
}

// UpdateHousehold changes the caller's own Household. Merge semantics, one
// statement, last write wins per field (BOOTSTRAP.md §4): a field left out is
// not touched, and two writers of different fields both land.
//
// The Household is the authenticated User's, taken from the context and never
// from the request, and the UPDATE filters on it in SQL.
func (h *Handlers) UpdateHousehold(ctx context.Context, request api.UpdateHouseholdRequestObject) (api.UpdateHouseholdResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		// Unreachable through the router: httpserver's sessionRequired answers
		// 401 for this operation first. Kept so a router change that lost the
		// check degrades to the contract's 401, not an UPDATE with a zero
		// household_id.
		return api.UpdateHousehold401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}
	body := request.Body
	if body == nil {
		// Unreachable: the contract's body is required, and the validator
		// refuses an absent one first.
		return api.UpdateHousehold400JSONResponse{
			BadRequestJSONResponse: api.BadRequestJSONResponse{Code: api.INVALIDJSONBODY},
		}, nil
	}

	params := db.UpdateHouseholdParams{
		HouseholdID:       user.HouseholdID,
		ReportingCurrency: body.ReportingCurrency,
	}
	if body.PeriodStartDay != nil {
		day := int32(*body.PeriodStartDay)
		params.PeriodStartDay = &day
	}

	// expected_monthly_income is the one property null clears, so it carries a
	// set flag beside its value (queries/household.sql): sent as null is
	// "set, to nothing", left out is "not set".
	if body.ExpectedMonthlyIncome.IsSpecified() {
		params.SetExpectedMonthlyIncome = true
		if text, err := body.ExpectedMonthlyIncome.Get(); err == nil {
			income, err := decimal.NewFromString(text)
			if err != nil {
				// Unreachable: the contract's pattern admits only what parses.
				return invalidField(failure{"expected_monthly_income", "pattern"}), nil
			}
			params.ExpectedMonthlyIncome = decimal.NullDecimal{Decimal: income, Valid: true}
		}
	}

	// The rules the contract cannot state. Every one that fails is collected
	// and the least (field, rule) reported, as the spec validator does for the
	// ones it can state. They are reached only once every contract rule has
	// passed: the validator answers first (BOOTSTRAP.md §5.1.1).
	var failures []failure
	if body.HouseholdDisplayName != nil {
		name, rule := auth.CheckHouseholdDisplayName(*body.HouseholdDisplayName)
		if rule != "" {
			failures = append(failures, failure{"household_display_name", rule})
		}
		params.HouseholdDisplayName = &name
	}
	if body.ReportingCurrency != nil {
		if err := httperr.Validator().Var(*body.ReportingCurrency, iso4217); err != nil {
			failures = append(failures, failure{"reporting_currency", iso4217})
		}
	}
	if len(failures) > 0 {
		first := failures[0]
		for _, f := range failures[1:] {
			if f.less(first) {
				first = f
			}
		}
		return invalidField(first), nil
	}

	row, err := h.q.UpdateHousehold(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// A User whose Household is gone cannot be served coherently, as in
			// GetMe.
			slog.Error("update household: household missing for user", "household_id", user.HouseholdID)
		} else {
			// Never the error's text beyond what the driver says: nothing here
			// logs a request body, and a Household's name is user content.
			slog.Error("update household", "err", err)
		}
		return internalError(), nil
	}
	return api.UpdateHousehold200JSONResponse(auth.HouseholdResponse(row)), nil
}

// iso4217 is go-playground/validator's tag for a currency code, and the rule
// reported when one is not (BOOTSTRAP.md §4's vocabulary is the validator's).
const iso4217 = "iso4217"

// failure is a field breaking a rule the contract cannot state.
type failure struct{ field, rule string }

func (f failure) less(o failure) bool {
	if f.field != o.field {
		return f.field < o.field
	}
	return f.rule < o.rule
}

func invalidField(f failure) api.UpdateHouseholdResponseObject {
	return api.UpdateHousehold400JSONResponse{
		BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: api.VALIDATION,
			Args: &map[string]interface{}{"field": f.field, "rule": f.rule},
		},
	}
}

// internalError is this package's own 500: auth's panics for a response type it
// does not list, and this one is not on it.
func internalError() api.UpdateHouseholdResponseObject {
	return api.UpdateHousehold500JSONResponse{
		InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
	}
}
