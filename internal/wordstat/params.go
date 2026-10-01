package wordstat

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Значения enum'ов из yandex/cloud/searchapi/v2/wordstat_service.proto.
// API строго проверяет префиксы: без PERIOD_/REGION_ вернётся INVALID_ARGUMENT.
const (
	PeriodDaily   = "PERIOD_DAILY"
	PeriodWeekly  = "PERIOD_WEEKLY"
	PeriodMonthly = "PERIOD_MONTHLY"

	RegionAll     = "REGION_ALL"
	RegionCities  = "REGION_CITIES"
	RegionRegions = "REGION_REGIONS"
)

const (
	// DefaultNumPhrases — сколько фраз вернуть, если numPhrases не задан (дефолт API).
	DefaultNumPhrases = 20
	// MaxNumPhrases — верхняя граница numPhrases по proto ((value) = "1-2000").
	MaxNumPhrases = 2000
)

const dateLayout = "2006-01-02"

// validatePhrase проверяет обязательное поле phrase (<=400 символов в proto).
func validatePhrase(phrase string) error {
	if strings.TrimSpace(phrase) == "" {
		return errors.New("phrase must not be empty")
	}
	return nil
}

// NormalizePeriod приводит пользовательское значение к enum API.
// Принимает daily/weekly/monthly в любом регистре, а также готовые PERIOD_*.
// Пустое значение → PERIOD_MONTHLY (самый полезный дефолт для сезонности).
func NormalizePeriod(period string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(period)) {
	case "", "MONTH", "MONTHLY", PeriodMonthly:
		return PeriodMonthly, nil
	case "DAY", "DAILY", PeriodDaily:
		return PeriodDaily, nil
	case "WEEK", "WEEKLY", PeriodWeekly:
		return PeriodWeekly, nil
	}
	return "", fmt.Errorf("invalid period %q: allowed daily, weekly, monthly", period)
}

// NormalizeRegion приводит пользовательское значение к enum API.
// Принимает all/cities/regions в любом регистре, а также готовые REGION_*.
// Пустое значение → REGION_ALL.
func NormalizeRegion(region string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(region)) {
	case "", "ALL", RegionAll:
		return RegionAll, nil
	case "CITY", "CITIES", RegionCities:
		return RegionCities, nil
	case "REGION", "REGIONS", RegionRegions:
		return RegionRegions, nil
	}
	return "", fmt.Errorf("invalid regionMode %q: allowed all, cities, regions", region)
}

// ParseDate принимает RFC3339 (2026-01-01T00:00:00Z) или YYYY-MM-DD и
// возвращает начало суток в UTC.
func ParseDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("empty date")
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return startOfDay(t.UTC()), nil
	}
	if t, err := time.Parse(dateLayout, value); err == nil {
		return startOfDay(t.UTC()), nil
	}
	return time.Time{}, fmt.Errorf("invalid date %q: expected RFC3339 (2026-01-01T00:00:00Z) or YYYY-MM-DD", value)
}

// ResolveDynamicsRange возвращает fromDate/toDate в RFC3339 для метода dynamics.
//
// API требует fromDate и накладывает ограничения на границы периода:
//   - monthly: fromDate — первый день месяца, toDate — последний день месяца;
//   - weekly:  fromDate — понедельник, toDate — воскресенье;
//   - daily:   доступны последние 60 дней.
//
// Незаданные границы досчитываются от now: 12 месяцев / 12 недель / 60 дней,
// заканчиваясь последним завершённым периодом.
func ResolveDynamicsRange(period, fromDate, toDate string, now time.Time) (string, string, error) {
	var (
		from, to       time.Time
		hasFrom, hasTo bool
		err            error
	)

	if strings.TrimSpace(fromDate) != "" {
		if from, err = ParseDate(fromDate); err != nil {
			return "", "", err
		}
		hasFrom = true
	}
	if strings.TrimSpace(toDate) != "" {
		if to, err = ParseDate(toDate); err != nil {
			return "", "", err
		}
		hasTo = true
	}

	today := startOfDay(now.UTC())
	firstOfThisMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)

	switch period {
	case PeriodMonthly:
		if hasFrom && from.Day() != 1 {
			return "", "", fmt.Errorf("for period=monthly fromDate must be the first day of a month, got %s", from.Format(dateLayout))
		}
		if hasTo && !to.Equal(lastDayOfMonth(to)) {
			return "", "", fmt.Errorf("for period=monthly toDate must be the last day of a month, got %s", to.Format(dateLayout))
		}
		if !hasTo {
			to = firstOfThisMonth.AddDate(0, 0, -1) // последний день предыдущего месяца
		}
		if !hasFrom {
			from = time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -11, 0)
		}
	case PeriodWeekly:
		if hasFrom && from.Weekday() != time.Monday {
			return "", "", fmt.Errorf("for period=weekly fromDate must be a Monday, got %s (%s)", from.Format(dateLayout), from.Weekday())
		}
		if hasTo && to.Weekday() != time.Sunday {
			return "", "", fmt.Errorf("for period=weekly toDate must be a Sunday, got %s (%s)", to.Format(dateLayout), to.Weekday())
		}
		if !hasTo {
			to = today.AddDate(0, 0, -int(today.Weekday())) // ближайшее воскресенье
		}
		if !hasFrom {
			from = to.AddDate(0, 0, -83) // 12 недель: понедельник 11 недель назад
		}
	case PeriodDaily:
		if !hasTo {
			to = today.AddDate(0, 0, -1)
		}
		if !hasFrom {
			from = to.AddDate(0, 0, -59) // 60 дней включительно
		}
	default:
		return "", "", fmt.Errorf("unsupported period %q", period)
	}

	if from.After(to) {
		return "", "", fmt.Errorf("fromDate %s is after toDate %s", from.Format(dateLayout), to.Format(dateLayout))
	}
	return from.Format(time.RFC3339), to.Format(time.RFC3339), nil
}

// startOfDay обнуляет время, оставляя дату в UTC.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// lastDayOfMonth возвращает начало последних суток месяца, в котором лежит t.
func lastDayOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, -1)
}
