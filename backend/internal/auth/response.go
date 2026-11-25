package auth

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/period"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// internalError is the only response an unexpected failure may produce. The
// cause is logged where it happened; the client is told nothing about it.
func internalError[T any]() (T, error) {
	// Switched on &zero, not zero: T is an interface, so zero is nil and a
	// type switch on it matches no case, and a nil response object makes the
	// strict handler write nothing at all, which net/http sends as a 200.
	var zero T
	var resp any
	switch any(&zero).(type) {
	case *api.LocalLoginResponseObject:
		resp = api.LocalLogin500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}
	case *api.GetMeResponseObject:
		resp = api.GetMe500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}
	case *api.LogoutResponseObject:
		resp = api.Logout500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}
	case *api.LocalRegisterResponseObject:
		resp = api.LocalRegister500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}
	case *api.GetOnboardingOptionsResponseObject:
		resp = api.GetOnboardingOptions500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}
	case *api.CommitOnboardingChoiceResponseObject:
		resp = api.CommitOnboardingChoice500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}
	case *api.UpdateMeResponseObject:
		resp = api.UpdateMe500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}
	case *api.ChangeLocalPasswordResponseObject:
		resp = api.ChangeLocalPassword500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}
	case *api.ChangeLocalEmailResponseObject:
		resp = api.ChangeLocalEmail500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}
	default:
		panic(fmt.Sprintf("auth: no 500 response for %T", &zero))
	}
	return resp.(T), nil
}

// formatDayStartsAt renders a Postgres `time` as HH:MM, the shape the contract
// declares. pgtype.Time carries microseconds since midnight, not a time.Time,
// because the column is a recurring wall-clock time with no date or zone
// attached — resolving it against a User's zone is the server's job elsewhere,
// and must not be faked here by inventing a date.
func formatDayStartsAt(t pgtype.Time) string {
	if !t.Valid {
		return "00:00"
	}
	minutes := t.Microseconds / int64(time.Minute/time.Microsecond)
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// meResponse projects the User, and their Household nested, onto the wire
// shape, with the caller's Today (#140 R15): the Period Day on the wall clock of
// the User's own time zone, less one day while that clock still reads earlier
// than the Household's day_starts_at. The same period.Today every handler that
// dates a request uses, so the answer and a date resolved by the server agree.
// The only error is a time zone the server cannot load, which the writes that
// set one refuse.
func (h *Handlers) meResponse(user db.User, household db.Household) (api.Me, error) {
	loc, err := time.LoadLocation(user.TimeZone)
	if err != nil {
		return api.Me{}, fmt.Errorf("load time zone: %w", err)
	}
	var dayStartsAt time.Duration
	if household.DayStartsAt.Valid {
		dayStartsAt = time.Duration(household.DayStartsAt.Microseconds) * time.Microsecond
	}
	today := period.Today(h.now(), loc, dayStartsAt)
	return api.Me{
		Id:              openapi_types.UUID(user.ID.Bytes),
		Email:           user.Email,
		UserDisplayName: user.UserDisplayName,
		Locale:          api.Locale(user.Locale),
		TimeZone:        user.TimeZone,
		Appearance:      api.Appearance(user.Appearance),
		Household:       HouseholdResponse(household),
		Today:           openapi_types.Date{Time: today.Time()},
	}, nil
}

// HouseholdResponse projects a Household onto the wire shape. It lives here,
// with GetMe's projection, and is exported for the household package's PATCH:
// that package imports auth (for the User in the context), so the dependency
// cannot run the other way.
//
// Money is a STRING at the storage scale, 4 decimal places, never trimmed
// (BOOTSTRAP.md §4) — StringFixed(4), not String(), which strips trailing
// zeros. decimal.NullDecimal.String() would also render "0" for a null,
// which is a different statement from "not supplied", so the null case
// returns no field at all.
func HouseholdResponse(household db.Household) api.Household {
	out := api.Household{
		Id:                   openapi_types.UUID(household.ID.Bytes),
		HouseholdDisplayName: household.HouseholdDisplayName,
		ReportingCurrency:    household.ReportingCurrency,
		PeriodStartDay:       int(household.PeriodStartDay),
		DayStartsAt:          formatDayStartsAt(household.DayStartsAt),
		AllowanceMode:        api.AllowanceMode(household.AllowanceMode),
	}
	if household.ExpectedMonthlyIncome.Valid {
		income := household.ExpectedMonthlyIncome.Decimal.StringFixed(4)
		out.ExpectedMonthlyIncome = &income
	}
	return out
}
