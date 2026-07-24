package server

import (
	"strconv"
	"strings"
	"time"
)

type cronSpec struct {
	minutes  map[int]bool
	hours    map[int]bool
	days     map[int]bool
	months   map[int]bool
	weekdays map[int]bool
}

func ValidCron5(expr string) bool {
	_, err := parseCron5(expr)
	return err == nil
}

func NextCronRun(expr string, from time.Time) time.Time {
	spec, err := parseCron5(expr)
	if err != nil {
		return time.Time{}
	}
	next := from.UTC().Truncate(time.Minute).Add(time.Minute)
	deadline := next.AddDate(3, 0, 0)
	for !next.After(deadline) {
		if spec.matches(next) {
			return next
		}
		next = next.Add(time.Minute)
	}
	return time.Time{}
}

func CronRunsBetween(expr string, from, to time.Time) []time.Time {
	runs := make([]time.Time, 0)
	next := NextCronRun(expr, from)
	for !next.IsZero() && !next.After(to) {
		runs = append(runs, next)
		next = NextCronRun(expr, next)
	}
	return runs
}

func parseCron5(expr string) (cronSpec, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return cronSpec{}, errInvalidCron
	}
	minutes, err := parseCronField(parts[0], 0, 59)
	if err != nil {
		return cronSpec{}, err
	}
	hours, err := parseCronField(parts[1], 0, 23)
	if err != nil {
		return cronSpec{}, err
	}
	days, err := parseCronField(parts[2], 1, 31)
	if err != nil {
		return cronSpec{}, err
	}
	months, err := parseCronField(parts[3], 1, 12)
	if err != nil {
		return cronSpec{}, err
	}
	weekdays, err := parseCronField(parts[4], 0, 6)
	if err != nil {
		return cronSpec{}, err
	}
	return cronSpec{minutes: minutes, hours: hours, days: days, months: months, weekdays: weekdays}, nil
}

func parseCronField(raw string, minValue, maxValue int) (map[int]bool, error) {
	values := map[int]bool{}
	if raw == "*" {
		for value := minValue; value <= maxValue; value++ {
			values[value] = true
		}
		return values, nil
	}
	for _, part := range strings.Split(raw, ",") {
		if part == "" {
			return nil, errInvalidCron
		}
		if strings.Contains(part, "/") {
			pieces := strings.Split(part, "/")
			if len(pieces) != 2 {
				return nil, errInvalidCron
			}
			step, err := strconv.Atoi(pieces[1])
			if err != nil || step <= 0 {
				return nil, errInvalidCron
			}
			rangeStart, rangeEnd, err := cronRange(pieces[0], minValue, maxValue)
			if err != nil {
				return nil, err
			}
			for value := rangeStart; value <= rangeEnd; value += step {
				values[value] = true
			}
			continue
		}
		rangeStart, rangeEnd, err := cronRange(part, minValue, maxValue)
		if err != nil {
			return nil, err
		}
		for value := rangeStart; value <= rangeEnd; value++ {
			values[value] = true
		}
	}
	return values, nil
}

func cronRange(raw string, minValue, maxValue int) (int, int, error) {
	if raw == "*" {
		return minValue, maxValue, nil
	}
	if strings.Contains(raw, "-") {
		pieces := strings.Split(raw, "-")
		if len(pieces) != 2 {
			return 0, 0, errInvalidCron
		}
		start, err := strconv.Atoi(pieces[0])
		if err != nil {
			return 0, 0, errInvalidCron
		}
		end, err := strconv.Atoi(pieces[1])
		if err != nil || start > end || start < minValue || end > maxValue {
			return 0, 0, errInvalidCron
		}
		return start, end, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minValue || value > maxValue {
		return 0, 0, errInvalidCron
	}
	return value, value, nil
}

func (spec cronSpec) matches(t time.Time) bool {
	weekday := int(t.Weekday())
	return spec.minutes[t.Minute()] &&
		spec.hours[t.Hour()] &&
		spec.days[t.Day()] &&
		spec.months[int(t.Month())] &&
		spec.weekdays[weekday]
}

var errInvalidCron = strconv.ErrSyntax
