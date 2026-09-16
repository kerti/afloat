package auth

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// internalError is the only response an unexpected failure may produce. The
// cause is logged where it happened; the client is told nothing about it.
func internalError[T any]() (T, error) {
	var zero T
	switch any(zero).(type) {
	case api.LocalLoginResponseObject:
		return any(api.LocalLogin500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}).(T), nil
	case api.GetMeResponseObject:
		return any(api.GetMe500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}).(T), nil
	case api.LogoutResponseObject:
		return any(api.Logout500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}).(T), nil
	}
	return zero, nil
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

// meResponse projects the User and Household onto the wire shape.
//
// Money is a STRING (BOOTSTRAP.md §4). decimal.NullDecimal.String() would
// render "0" for a null, which is a different statement from "not supplied", so
// the null case returns no field at all.
func meResponse(user db.User, household db.Household) api.Me {
	me := api.Me{
		Id:                   openapi_types.UUID(user.ID.Bytes),
		HouseholdId:          openapi_types.UUID(user.HouseholdID.Bytes),
		Email:                openapi_types.Email(user.Email),
		DisplayName:          user.DisplayName,
		Locale:               api.Locale(user.Locale),
		TimeZone:             user.TimeZone,
		HouseholdDisplayName: household.DisplayName,
		ReportingCurrency:    household.ReportingCurrency,
		PeriodStartDay:       int(household.PeriodStartDay),
		DayStartsAt:          formatDayStartsAt(household.DayStartsAt),
		AllowanceMode:        api.MeAllowanceMode(household.AllowanceMode),
	}
	if household.ExpectedMonthlyIncome.Valid {
		income := household.ExpectedMonthlyIncome.Decimal.String()
		me.ExpectedMonthlyIncome = &income
	}
	return me
}
