package costreasoning

import (
	"errors"
	"fmt"
	"time"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type analysisWindows struct {
	currentStart  time.Time
	currentEnd    time.Time
	baselineStart time.Time
	baselineEnd   time.Time
	historyStart  time.Time
	complete      bool
	currentDays   int
	baselineDays  int
}

// baselineScale is the factor that turns baseline totals into the current
// period's length, so periods of different lengths compare daily rates.
func (w analysisWindows) baselineScale() float64 {
	if w.baselineDays <= 0 || w.baselineDays == w.currentDays {
		return 1
	}
	return float64(w.currentDays) / float64(w.baselineDays)
}

// Comparison strategies for gcp_cost_explain.
const (
	comparisonPreviousPeriod = "previous_period"
	comparisonYearOverYear   = "year_over_year"
	comparisonCustom         = "custom"
)

func parseLocalDate(name, value string, location *time.Location) (time.Time, error) {
	date, err := time.ParseInLocation("2006-01-02", value, location)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s %q", name, value)
	}
	return date, nil
}

func resolveWindows(req models.ExplainCostRequest, now time.Time, historyDays int) (analysisWindows, error) {
	location, err := time.LoadLocation(req.Timezone)
	if err != nil {
		return analysisWindows{}, err
	}
	localNow := now.In(location)
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	var currentStart, currentEnd time.Time
	switch req.Period {
	case "last_7_complete_days":
		currentEnd = today
		currentStart = currentEnd.AddDate(0, 0, -7)
	case "last_30_complete_days":
		currentEnd = today
		currentStart = currentEnd.AddDate(0, 0, -30)
	case "month_to_date":
		currentEnd = today
		currentStart = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, location)
		if !currentStart.Before(currentEnd) {
			return analysisWindows{}, errors.New("month_to_date has no complete days yet")
		}
	case "custom":
		if req.StartDate == "" || req.EndDate == "" {
			return analysisWindows{}, errors.New("start_date and end_date are required for a custom period")
		}
		startDate, err := parseLocalDate("start_date", req.StartDate, location)
		if err != nil {
			return analysisWindows{}, err
		}
		endDate, err := parseLocalDate("end_date", req.EndDate, location)
		if err != nil {
			return analysisWindows{}, err
		}
		currentStart = startDate
		currentEnd = endDate.AddDate(0, 0, 1) // end_date is inclusive for users.
		if currentEnd.After(today) {
			return analysisWindows{}, errors.New("custom end_date must be before today so the period is complete")
		}
	default:
		return analysisWindows{}, errors.New("period must be last_7_complete_days, last_30_complete_days, month_to_date, or custom")
	}
	if !currentStart.Before(currentEnd) {
		return analysisWindows{}, errors.New("cost period must contain at least one complete day")
	}
	days := calendarDays(currentStart, currentEnd)
	if days > 366 {
		return analysisWindows{}, errors.New("cost period must not exceed 366 complete days")
	}
	baselineStart, baselineEnd, err := resolveBaseline(req, currentStart, currentEnd, days, today, location)
	if err != nil {
		return analysisWindows{}, err
	}
	// The daily history covers historyDays before the current period ends.
	// A previous-period baseline is adjacent, so the history also reaches
	// back to its start; a year-over-year or custom baseline is collected
	// separately and does not stretch the daily history.
	historyStart := currentEnd.AddDate(0, 0, -historyDays)
	if req.Comparison == comparisonPreviousPeriod || req.Comparison == "" {
		if historyStart.After(baselineStart) {
			historyStart = baselineStart
		}
	} else if historyStart.After(currentStart) {
		historyStart = currentStart
	}
	return analysisWindows{
		currentStart: currentStart.UTC(), currentEnd: currentEnd.UTC(), baselineStart: baselineStart.UTC(),
		baselineEnd: baselineEnd.UTC(), historyStart: historyStart.UTC(), complete: true,
		currentDays: days, baselineDays: calendarDays(baselineStart, baselineEnd),
	}, nil
}

// resolveBaseline returns the comparison window: the equal period right
// before the current one, the same dates one year earlier, or caller-chosen
// complete days that end no later than the current period starts.
func resolveBaseline(req models.ExplainCostRequest, currentStart, currentEnd time.Time, days int, today time.Time, location *time.Location) (time.Time, time.Time, error) {
	switch req.Comparison {
	case comparisonPreviousPeriod, "":
		return currentStart.AddDate(0, 0, -days), currentStart, nil
	case comparisonYearOverYear:
		return currentStart.AddDate(-1, 0, 0), currentEnd.AddDate(-1, 0, 0), nil
	case comparisonCustom:
		if req.BaselineStartDate == "" || req.BaselineEndDate == "" {
			return time.Time{}, time.Time{}, errors.New("baseline_start_date and baseline_end_date are required when comparison=custom")
		}
		start, err := parseLocalDate("baseline_start_date", req.BaselineStartDate, location)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		endDate, err := parseLocalDate("baseline_end_date", req.BaselineEndDate, location)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		end := endDate.AddDate(0, 0, 1) // baseline_end_date is inclusive for users.
		if !start.Before(end) {
			return time.Time{}, time.Time{}, errors.New("baseline_start_date must not be after baseline_end_date")
		}
		if end.After(currentStart) {
			return time.Time{}, time.Time{}, errors.New("the custom baseline must end before the current period starts")
		}
		if end.After(today) {
			return time.Time{}, time.Time{}, errors.New("the custom baseline must contain only complete days")
		}
		if calendarDays(start, end) > 366 {
			return time.Time{}, time.Time{}, errors.New("the custom baseline must not exceed 366 complete days")
		}
		return start, end, nil
	default:
		return time.Time{}, time.Time{}, errors.New("comparison must be previous_period, year_over_year, or custom")
	}
}

func calendarDays(start, end time.Time) int {
	days := 0
	for cursor := start; cursor.Before(end); cursor = cursor.AddDate(0, 0, 1) {
		days++
	}
	return days
}
