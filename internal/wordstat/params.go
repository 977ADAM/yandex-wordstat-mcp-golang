package wordstat

import (
	"fmt"
	"strings"
	"time"
)

// Значения enum'ов из yandex/cloud/searchapi/v2/wordstat_service.proto.
// API строго проверяет префиксы: без PERIOD_/REGION_/DEVICE_ вернётся INVALID_ARGUMENT.
const (
	PeriodDaily   = "PERIOD_DAILY"
	PeriodWeekly  = "PERIOD_WEEKLY"
	PeriodMonthly = "PERIOD_MONTHLY"

	RegionAll     = "REGION_ALL"
	RegionCities  = "REGION_CITIES"
	RegionRegions = "REGION_REGIONS"

	DeviceAll     = "DEVICE_ALL"
	DeviceDesktop = "DEVICE_DESKTOP"
	DevicePhone   = "DEVICE_PHONE"
	DeviceTablet  = "DEVICE_TABLET"
)

const (
	// DefaultNumPhrases — сколько фраз вернуть, если numPhrases не задан (дефолт API).
	DefaultNumPhrases = 20
	// MaxNumPhrases — верхняя граница numPhrases по proto ((value) = "1-2000").
	MaxNumPhrases = 2000
	// MaxRegions — верхняя граница списка регионов по proto ((size) = "<=100").
	MaxRegions = 100
	// MaxDevices — верхняя граница списка устройств по proto ((size) = "<=3").
	MaxDevices = 3
)

const dateLayout = "2006-01-02"

// TopParams — параметры запроса top_requests.
type TopParams struct {
	Phrase     string
	NumPhrases int
	// Regions — geo ID Яндекса: "213" — Москва, "1" — Москва и область,
	// "225" — Россия. Пусто — вся Россия.
	Regions []string
	// Devices — all | desktop | phone | tablet. Пусто — все устройства.
	Devices []string
}

// DynamicsParams — параметры запроса dynamics.
type DynamicsParams struct {
	Phrase   string
	Period   string
	FromDate string
	ToDate   string
	Regions  []string
	Devices  []string
}

// validatePhrase проверяет обязательное поле phrase (<=400 символов в proto).
func validatePhrase(phrase string) error {
	if strings.TrimSpace(phrase) == "" {
		return fmt.Errorf("%w: phrase must not be empty", ErrInvalidArgument)
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
	return "", fmt.Errorf("%w: invalid period %q: allowed daily, weekly, monthly", ErrInvalidArgument, period)
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
	return "", fmt.Errorf("%w: invalid regionMode %q: allowed all, cities, regions", ErrInvalidArgument, region)
}

// NormalizeDevices приводит список устройств к enum API: принимает
// all/desktop/phone/tablet в любом регистре и готовые DEVICE_*, убирает дубли
// (порядок первого вхождения сохраняется) и проверяет лимит proto.
// Пустой список — валидное «все устройства».
func NormalizeDevices(devices []string) ([]string, error) {
	if len(devices) == 0 {
		return nil, nil
	}

	out := make([]string, 0, len(devices))
	seen := make(map[string]bool, len(devices))
	for _, device := range devices {
		normalized, err := normalizeDevice(device)
		if err != nil {
			return nil, err
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}

	if len(out) > MaxDevices {
		return nil, fmt.Errorf("%w: too many devices: %d, allowed at most %d", ErrInvalidArgument, len(out), MaxDevices)
	}
	return out, nil
}

// normalizeDevice приводит одно значение к enum API.
func normalizeDevice(device string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(device)) {
	case "ALL", DeviceAll:
		return DeviceAll, nil
	case "DESKTOP", DeviceDesktop:
		return DeviceDesktop, nil
	case "PHONE", DevicePhone:
		return DevicePhone, nil
	case "TABLET", DeviceTablet:
		return DeviceTablet, nil
	}
	return "", fmt.Errorf("%w: invalid device %q: allowed all, desktop, phone, tablet", ErrInvalidArgument, device)
}

// ValidateRegions проверяет geo ID регионов (только цифры), убирает дубли
// (порядок первого вхождения сохраняется) и проверяет лимит proto.
// Пустой список — валидное «вся Россия».
func ValidateRegions(regions []string) ([]string, error) {
	if len(regions) == 0 {
		return nil, nil
	}

	out := make([]string, 0, len(regions))
	seen := make(map[string]bool, len(regions))
	for _, region := range regions {
		id := strings.TrimSpace(region)
		if id == "" || !isDigits(id) {
			return nil, fmt.Errorf("%w: invalid region %q: expected numeric geo id", ErrInvalidArgument, region)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}

	if len(out) > MaxRegions {
		return nil, fmt.Errorf("%w: too many regions: %d, allowed at most %d", ErrInvalidArgument, len(out), MaxRegions)
	}
	return out, nil
}

// isDigits сообщает, состоит ли строка только из цифр (пустая — нет).
func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// ParseDate принимает RFC3339 (2026-01-01T00:00:00Z) или YYYY-MM-DD и
// возвращает начало суток в UTC.
func ParseDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("%w: empty date", ErrInvalidArgument)
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return startOfDay(t.UTC()), nil
	}
	if t, err := time.Parse(dateLayout, value); err == nil {
		return startOfDay(t.UTC()), nil
	}
	return time.Time{}, fmt.Errorf("%w: invalid date %q: expected RFC3339 (2026-01-01T00:00:00Z) or YYYY-MM-DD", ErrInvalidArgument, value)
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
			return "", "", fmt.Errorf("%w: for period=monthly fromDate must be the first day of a month, got %s", ErrInvalidArgument, from.Format(dateLayout))
		}
		if hasTo && !to.Equal(lastDayOfMonth(to)) {
			return "", "", fmt.Errorf("%w: for period=monthly toDate must be the last day of a month, got %s", ErrInvalidArgument, to.Format(dateLayout))
		}
		if !hasTo {
			to = firstOfThisMonth.AddDate(0, 0, -1) // последний день предыдущего месяца
		}
		if !hasFrom {
			from = time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -11, 0)
		}
	case PeriodWeekly:
		if hasFrom && from.Weekday() != time.Monday {
			return "", "", fmt.Errorf("%w: for period=weekly fromDate must be a Monday, got %s (%s)", ErrInvalidArgument, from.Format(dateLayout), from.Weekday())
		}
		if hasTo && to.Weekday() != time.Sunday {
			return "", "", fmt.Errorf("%w: for period=weekly toDate must be a Sunday, got %s (%s)", ErrInvalidArgument, to.Format(dateLayout), to.Weekday())
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
		return "", "", fmt.Errorf("%w: unsupported period %q", ErrInvalidArgument, period)
	}

	if from.After(to) {
		return "", "", fmt.Errorf("%w: fromDate %s is after toDate %s", ErrInvalidArgument, from.Format(dateLayout), to.Format(dateLayout))
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
