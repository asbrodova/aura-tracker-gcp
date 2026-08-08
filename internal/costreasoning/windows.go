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
		startDate, err := time.ParseInLocation("2006-01-02", req.StartDate, location)
		if err != nil {
			return analysisWindows{}, fmt.Errorf("invalid start_date %q", req.StartDate)
		}
		endDate, err := time.ParseInLocation("2006-01-02", req.EndDate, location)
		if err != nil {
			return analysisWindows{}, fmt.Errorf("invalid end_date %q", req.EndDate)
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
	baselineEnd := currentStart
	baselineStart := baselineEnd.AddDate(0, 0, -days)
	historyStart := currentEnd.AddDate(0, 0, -historyDays)
	if historyStart.After(baselineStart) {
		historyStart = baselineStart
	}
	return analysisWindows{
		currentStart: currentStart.UTC(), currentEnd: currentEnd.UTC(), baselineStart: baselineStart.UTC(),
		baselineEnd: baselineEnd.UTC(), historyStart: historyStart.UTC(), complete: true,
	}, nil
}

func calendarDays(start, end time.Time) int {
	days := 0
	for cursor := start; cursor.Before(end); cursor = cursor.AddDate(0, 0, 1) {
		days++
	}
	return days
}
