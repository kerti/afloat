// Package period is Period Day and Budget Period arithmetic: which day Today
// is, which Budget Period a day belongs to, and where a Period starts and ends.
//
// It is the only place this arithmetic lives (BOOTSTRAP.md §4): the client
// derives none of it, because the two would disagree at 03:59. Like trajectory
// it is pure: no database, no clock read. The caller hands it an instant, so a
// test pins exact dates and production passes time.Now.
// contract/testdata/periods.json pins the answers.
package period

import (
	"errors"
	"fmt"
	"time"
)

// Date is a calendar date with no time of day and no zone: a Period Day, a
// Period's first or last day, an effective_on. A bare time.Time would carry a
// zone that means nothing here and compare unequal for the same day.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// ParseDate reads the canonical YYYY-MM-DD spelling and nothing else.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Date{}, fmt.Errorf("period: %q is not a YYYY-MM-DD date", s)
	}
	return DateOf(t), nil
}

// DateOf is the calendar date t reads as, in t's own zone.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{y, m, d}
}

// String is the canonical YYYY-MM-DD spelling, the one the wire and the
// database use.
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// Time is midnight UTC of the date: the value a `date` column round-trips as.
func (d Date) Time() time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// AddDays moves the date by n calendar days, which may be negative.
func (d Date) AddDays(n int) Date {
	return DateOf(time.Date(d.Year, d.Month, d.Day+n, 0, 0, 0, 0, time.UTC))
}

// Before reports whether d is a strictly earlier day than o.
func (d Date) Before(o Date) bool {
	return d.Time().Before(o.Time())
}

// ParseDayStartsAt reads a Household's `day_starts_at`, `HH:MM`, as the time
// after local midnight at which a Period Day begins.
func ParseDayStartsAt(s string) (time.Duration, error) {
	// The layout's 15 takes one digit as well as two, so "4:00" parses; the
	// length check keeps the spelling to the contract's HH:MM.
	t, err := time.Parse("15:04", s)
	if err != nil || len(s) != 5 {
		return 0, fmt.Errorf("period: %q is not an HH:MM day_starts_at", s)
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

// Today is the Period Day containing now: the date on the wall clock of the
// User's zone, less one day while that wall clock still reads earlier than
// dayStartsAt (CONTEXT.md §Time). 01:00 local with a 04:00 start is still
// yesterday.
//
// It compares the wall clock's time of day, not an elapsed duration, so a
// daylight-saving jump cannot move where the day ends: 03:59:59 local is
// the previous Period Day whether or not an hour was skipped or repeated that
// night.
func Today(now time.Time, loc *time.Location, dayStartsAt time.Duration) Date {
	local := now.In(loc)
	h, m, s := local.Clock()
	sinceMidnight := time.Duration(h)*time.Hour + time.Duration(m)*time.Minute +
		time.Duration(s)*time.Second + time.Duration(local.Nanosecond())
	day := DateOf(local)
	if sinceMidnight < dayStartsAt {
		return day.AddDays(-1)
	}
	return day
}

// Period is one Budget Period: its label and its window, both ends inclusive.
type Period struct {
	// YearMonth is the YYYY-MM of the calendar month in which the Period
	// starts (CONTEXT.md §Time), which is its identity.
	YearMonth string
	StartsOn  Date
	EndsOn    Date
}

// Contains reports whether the Period Day d falls inside the window.
func (p Period) Contains(d Date) bool {
	return !d.Before(p.StartsOn) && !p.EndsOn.Before(d)
}

// errStartDay is what an out-of-range period_start_day is. The database holds
// 1 to 28 behind a CHECK, so this is a corrupt row or a caller's bug.
var errStartDay = errors.New("period: period_start_day must be 1 to 28")

// Of is the Budget Period that day d belongs to when a Period opens on
// periodStartDay: that day's month, or the previous one while the day is
// before the start day.
func Of(d Date, periodStartDay int) (Period, error) {
	if periodStartDay < 1 || periodStartDay > 28 {
		return Period{}, errStartDay
	}
	month := time.Date(d.Year, d.Month, 1, 0, 0, 0, 0, time.UTC)
	if d.Day < periodStartDay {
		month = month.AddDate(0, -1, 0)
	}
	return window(month.Year(), month.Month(), periodStartDay), nil
}

// Window is the window of a stored label under the Household's current
// periodStartDay. Nothing records the value a Period was lived under, so a
// change of the setting re-cuts every Period at once (#113 D1).
func Window(yearMonth string, periodStartDay int) (Period, error) {
	if periodStartDay < 1 || periodStartDay > 28 {
		return Period{}, errStartDay
	}
	t, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		return Period{}, fmt.Errorf("period: %q is not a YYYY-MM label", yearMonth)
	}
	return window(t.Year(), t.Month(), periodStartDay), nil
}

// window runs from startDay of the month to the day before startDay of the
// next. startDay is at most 28, a day every month has, so neither end is ever
// clamped or skipped.
func window(year int, month time.Month, startDay int) Period {
	starts := Date{year, month, startDay}
	next := DateOf(time.Date(year, month+1, startDay, 0, 0, 0, 0, time.UTC))
	return Period{
		YearMonth: fmt.Sprintf("%04d-%02d", year, int(month)),
		StartsOn:  starts,
		EndsOn:    next.AddDays(-1),
	}
}
