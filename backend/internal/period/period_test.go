package period_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/kerti/afloat/backend/internal/period"
)

// Period Day and Budget Period arithmetic as pinned by
// contract/testdata/periods.json (BOOTSTRAP.md §6, issue #130). The suite keeps
// no copy of the numbers: every expected value is decoded from the fixture.
type fixture struct {
	Today []struct {
		Name        string `json:"name"`
		Now         string `json:"now"`
		TimeZone    string `json:"time_zone"`
		DayStartsAt string `json:"day_starts_at"`
		Expected    string `json:"expected"`
	} `json:"today"`
	Periods []struct {
		Name           string `json:"name"`
		Date           string `json:"date"`
		PeriodStartDay int    `json:"period_start_day"`
		YearMonth      string `json:"year_month"`
		StartsOn       string `json:"starts_on"`
		EndsOn         string `json:"ends_on"`
	} `json:"periods"`
	Windows []struct {
		YearMonth      string `json:"year_month"`
		PeriodStartDay int    `json:"period_start_day"`
		StartsOn       string `json:"starts_on"`
		EndsOn         string `json:"ends_on"`
	} `json:"windows"`
	Days []struct {
		Name           string `json:"name"`
		Today          string `json:"today"`
		PeriodStartDay int    `json:"period_start_day"`
		Days           int    `json:"days"`
		Elapsed        int    `json:"elapsed"`
		Remaining      int    `json:"remaining"`
	} `json:"days"`
	InvalidLabels         []string `json:"invalid_labels"`
	InvalidPeriodStartDay []int    `json:"invalid_period_start_days"`
}

func load(t *testing.T) fixture {
	t.Helper()
	raw, err := os.ReadFile("../../../contract/testdata/periods.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(f.Today) == 0 || len(f.Periods) == 0 || len(f.Windows) == 0 || len(f.Days) == 0 {
		t.Fatal("fixture has an empty section: this suite would check nothing")
	}
	return f
}

func date(t *testing.T, s string) period.Date {
	t.Helper()
	d, err := period.ParseDate(s)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", s, err)
	}
	return d
}

func TestTodayMatchesTheFixture(t *testing.T) {
	for _, row := range load(t).Today {
		t.Run(row.Name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, row.Now)
			if err != nil {
				t.Fatalf("now: %v", err)
			}
			loc, err := time.LoadLocation(row.TimeZone)
			if err != nil {
				t.Fatalf("zone %q: %v", row.TimeZone, err)
			}
			start, err := period.ParseDayStartsAt(row.DayStartsAt)
			if err != nil {
				t.Fatalf("day_starts_at: %v", err)
			}
			if got := period.Today(now, loc, start); got != date(t, row.Expected) {
				t.Errorf("Today = %s, want %s", got, row.Expected)
			}
		})
	}
}

func TestOfMatchesTheFixture(t *testing.T) {
	for _, row := range load(t).Periods {
		t.Run(row.Name, func(t *testing.T) {
			got, err := period.Of(date(t, row.Date), row.PeriodStartDay)
			if err != nil {
				t.Fatalf("Of: %v", err)
			}
			if got.YearMonth != row.YearMonth || got.StartsOn != date(t, row.StartsOn) || got.EndsOn != date(t, row.EndsOn) {
				t.Errorf("Of = %s %s..%s, want %s %s..%s", got.YearMonth, got.StartsOn, got.EndsOn, row.YearMonth, row.StartsOn, row.EndsOn)
			}
			if !got.Contains(date(t, row.Date)) {
				t.Errorf("the Period %s..%s does not contain the date %s it was made from", got.StartsOn, got.EndsOn, row.Date)
			}
		})
	}
}

func TestWindowMatchesTheFixture(t *testing.T) {
	for _, row := range load(t).Windows {
		t.Run(row.YearMonth, func(t *testing.T) {
			got, err := period.Window(row.YearMonth, row.PeriodStartDay)
			if err != nil {
				t.Fatalf("Window: %v", err)
			}
			if got.YearMonth != row.YearMonth || got.StartsOn != date(t, row.StartsOn) || got.EndsOn != date(t, row.EndsOn) {
				t.Errorf("Window = %s..%s, want %s..%s", got.StartsOn, got.EndsOn, row.StartsOn, row.EndsOn)
			}
		})
	}
}

// D, e and r (#155): the Period's length, the days before Today inside it and
// the days left counting Today.
func TestDayCountsMatchTheFixture(t *testing.T) {
	for _, row := range load(t).Days {
		t.Run(row.Name, func(t *testing.T) {
			today := date(t, row.Today)
			p, err := period.Of(today, row.PeriodStartDay)
			if err != nil {
				t.Fatalf("Of: %v", err)
			}
			d := p.Days()
			e := today.Sub(p.StartsOn)
			r := d - e
			if d != row.Days || e != row.Elapsed || r != row.Remaining {
				t.Errorf("D, e, r = %d, %d, %d; want %d, %d, %d", d, e, r, row.Days, row.Elapsed, row.Remaining)
			}
		})
	}
}

// The fixture pins boundaries; this is the property that makes them
// sufficient: on every day of every Period, 0 <= e < D and r counts Today.
func TestEveryDayOfTwoYearsHasConsistentDayCountsForEveryStartDay(t *testing.T) {
	for startDay := 1; startDay <= 28; startDay++ {
		for d := date(t, "2027-01-01"); d.Before(date(t, "2029-01-01")); d = d.AddDays(1) {
			p, err := period.Of(d, startDay)
			if err != nil {
				t.Fatalf("start day %d, %s: %v", startDay, d, err)
			}
			days, e := p.Days(), d.Sub(p.StartsOn)
			if e < 0 || e >= days || days-e < 1 {
				t.Fatalf("start day %d, %s: D=%d e=%d r=%d", startDay, d, days, e, days-e)
			}
			if days < 28 || days > 31 {
				t.Fatalf("start day %d, %s: a Period of %d days", startDay, d, days)
			}
		}
	}
}

func TestSubIsTheSignedDayDifference(t *testing.T) {
	a, b := date(t, "2028-02-28"), date(t, "2028-03-01")
	if got := b.Sub(a); got != 2 {
		t.Errorf("2028-03-01 - 2028-02-28 = %d, want 2 (a leap February)", got)
	}
	if got := a.Sub(b); got != -2 {
		t.Errorf("2028-02-28 - 2028-03-01 = %d, want -2", got)
	}
	if got := a.Sub(a); got != 0 {
		t.Errorf("a day less itself = %d, want 0", got)
	}
}

func TestWindowAndOfRefuseWhatIsNotAPeriod(t *testing.T) {
	f := load(t)
	for _, label := range f.InvalidLabels {
		if _, err := period.Window(label, 1); err == nil {
			t.Errorf("Window(%q) = nil error, want one", label)
		}
	}
	for _, day := range f.InvalidPeriodStartDay {
		if _, err := period.Window("2026-11", day); err == nil {
			t.Errorf("Window with period_start_day %d = nil error, want one", day)
		}
		if _, err := period.Of(date(t, "2026-11-15"), day); err == nil {
			t.Errorf("Of with period_start_day %d = nil error, want one", day)
		}
	}
}

func TestEveryDateOfAYearBelongsToExactlyOnePeriodForEveryStartDay(t *testing.T) {
	// The fixture pins boundaries; this is the property that makes them
	// sufficient: Periods tile the calendar with no gap and no overlap.
	for startDay := 1; startDay <= 28; startDay++ {
		prev := period.Period{}
		for d := date(t, "2027-01-01"); d.Before(date(t, "2029-01-01")); d = d.AddDays(1) {
			p, err := period.Of(d, startDay)
			if err != nil {
				t.Fatalf("start day %d, %s: %v", startDay, d, err)
			}
			if !p.Contains(d) {
				t.Fatalf("start day %d: %s..%s does not contain %s", startDay, p.StartsOn, p.EndsOn, d)
			}
			if prev.YearMonth != "" && prev.YearMonth != p.YearMonth && prev.EndsOn.AddDays(1) != p.StartsOn {
				t.Fatalf("start day %d: Period %s ends %s but %s starts %s", startDay, prev.YearMonth, prev.EndsOn, p.YearMonth, p.StartsOn)
			}
			prev = p
		}
	}
}

func TestParseDayStartsAt(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"00:00": 0,
		"04:00": 4 * time.Hour,
		"06:30": 6*time.Hour + 30*time.Minute,
		"23:59": 23*time.Hour + 59*time.Minute,
	} {
		got, err := period.ParseDayStartsAt(in)
		if err != nil || got != want {
			t.Errorf("ParseDayStartsAt(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "4:00", "24:00", "04:60", "04:00:00", "0400", "ab:cd"} {
		if _, err := period.ParseDayStartsAt(bad); err == nil {
			t.Errorf("ParseDayStartsAt(%q) = nil error, want one", bad)
		}
	}
}

func TestDateFormatsAndParses(t *testing.T) {
	d := period.Date{Year: 2026, Month: time.March, Day: 5}
	if got := d.String(); got != "2026-03-05" {
		t.Errorf("String = %q, want 2026-03-05", got)
	}
	for _, bad := range []string{"", "2026-3-5", "2026-02-30", "2026-13-01", "26-03-05", "2026-03-05T00:00:00Z", " 2026-03-05"} {
		if _, err := period.ParseDate(bad); err == nil {
			t.Errorf("ParseDate(%q) = nil error, want one", bad)
		}
	}
	if got := d.AddDays(30); got.String() != "2026-04-04" {
		t.Errorf("AddDays(30) = %s, want 2026-04-04", got)
	}
	if got := d.AddDays(-5); got.String() != "2026-02-28" {
		t.Errorf("AddDays(-5) = %s, want 2026-02-28", got)
	}
	if !d.Before(d.AddDays(1)) || d.Before(d) || d.AddDays(1).Before(d) {
		t.Error("Before is not a strict order")
	}
}
