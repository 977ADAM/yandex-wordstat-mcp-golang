// Package wordstat_test — чёрный ящик для чистых хелперов параметров Wordstat:
// нормализация period/region и расчёт границ периода для dynamics.
package wordstat_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"strconv"

	"github.com/977ADAM/yandex-wordstat-mcp-golang/internal/wordstat"
)

// dateLayout — формат даты для сообщений об ошибках в тестах.
const dateLayout = "2006-01-02"

func TestNormalizePeriod(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: wordstat.PeriodMonthly},
		{in: "monthly", want: wordstat.PeriodMonthly},
		{in: "MONTHLY", want: wordstat.PeriodMonthly},
		{in: " PERIOD_MONTHLY ", want: wordstat.PeriodMonthly},
		{in: "daily", want: wordstat.PeriodDaily},
		{in: "DAY", want: wordstat.PeriodDaily},
		{in: "Period_Daily", want: wordstat.PeriodDaily},
		{in: "weekly", want: wordstat.PeriodWeekly},
		{in: "week", want: wordstat.PeriodWeekly},
		{in: "yearly", wantErr: true},
		{in: "PERIOD_UNSPECIFIED", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := wordstat.NormalizePeriod(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizePeriod(%q) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizePeriod(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("NormalizePeriod(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeRegion(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: wordstat.RegionAll},
		{in: "all", want: wordstat.RegionAll},
		{in: "REGION_ALL", want: wordstat.RegionAll},
		{in: "cities", want: wordstat.RegionCities},
		{in: "City", want: wordstat.RegionCities},
		{in: "regions", want: wordstat.RegionRegions},
		{in: "REGION_REGIONS", want: wordstat.RegionRegions},
		{in: "districts", wantErr: true},
		{in: "REGION_UNSPECIFIED", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := wordstat.NormalizeRegion(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeRegion(%q) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeRegion(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("NormalizeRegion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		in      string
		want    string // RFC3339
		wantErr bool
	}{
		{in: "2026-01-01", want: "2026-01-01T00:00:00Z"},
		{in: "2026-01-01T10:20:30Z", want: "2026-01-01T00:00:00Z"},
		{in: "2026-01-01T13:20:30+03:00", want: "2026-01-01T00:00:00Z"},
		{in: "", wantErr: true},
		{in: "01.01.2026", wantErr: true},
		{in: "2026-13-01", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := wordstat.ParseDate(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseDate(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDate(%q) unexpected error: %v", tt.in, err)
			}
			if s := got.Format(time.RFC3339); s != tt.want {
				t.Errorf("ParseDate(%q) = %s, want %s", tt.in, s, tt.want)
			}
		})
	}
}

func TestResolveDynamicsRange(t *testing.T) {
	// четверг, 1 октября 2026
	now := time.Date(2026, 10, 1, 12, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		period   string
		from     string
		to       string
		wantFrom string
		wantTo   string
		wantErr  string
	}{
		{
			name:     "monthly: дефолт — прошлый завершённый месяц, окно 12 месяцев",
			period:   wordstat.PeriodMonthly,
			wantFrom: "2025-10-01T00:00:00Z",
			wantTo:   "2026-09-30T00:00:00Z",
		},
		{
			name:     "monthly: границы не меняются",
			period:   wordstat.PeriodMonthly,
			from:     "2026-01-01T00:00:00Z",
			to:       "2026-03-31T00:00:00Z",
			wantFrom: "2026-01-01T00:00:00Z",
			wantTo:   "2026-03-31T00:00:00Z",
		},
		{
			name:     "monthly: только toDate — from считается от него",
			period:   wordstat.PeriodMonthly,
			to:       "2026-06-30",
			wantFrom: "2025-07-01T00:00:00Z",
			wantTo:   "2026-06-30T00:00:00Z",
		},
		{
			name:    "monthly: fromDate не первое число",
			period:  wordstat.PeriodMonthly,
			from:    "2026-01-15",
			wantErr: "must be the first day of a month",
		},
		{
			name:    "monthly: toDate не последнее число",
			period:  wordstat.PeriodMonthly,
			from:    "2026-01-01",
			to:      "2026-03-15",
			wantErr: "must be the last day of a month",
		},
		{
			name:     "weekly: дефолт — от понедельника до последнего воскресенья",
			period:   wordstat.PeriodWeekly,
			wantFrom: "2026-07-06T00:00:00Z",
			wantTo:   "2026-09-27T00:00:00Z",
		},
		{
			name:     "weekly: только toDate — from считается от него",
			period:   wordstat.PeriodWeekly,
			to:       "2026-06-28",
			wantFrom: "2026-04-06T00:00:00Z",
			wantTo:   "2026-06-28T00:00:00Z",
		},
		{
			name:    "weekly: fromDate не понедельник",
			period:  wordstat.PeriodWeekly,
			from:    "2026-03-03",
			wantErr: "must be a Monday",
		},
		{
			name:    "weekly: toDate не воскресенье",
			period:  wordstat.PeriodWeekly,
			from:    "2026-03-02",
			to:      "2026-04-04",
			wantErr: "must be a Sunday",
		},
		{
			name:     "daily: дефолт — последние 60 дней",
			period:   wordstat.PeriodDaily,
			wantFrom: "2026-08-02T00:00:00Z",
			wantTo:   "2026-09-30T00:00:00Z",
		},
		{
			name:     "daily: только toDate",
			period:   wordstat.PeriodDaily,
			to:       "2026-05-10",
			wantFrom: "2026-03-12T00:00:00Z",
			wantTo:   "2026-05-10T00:00:00Z",
		},
		{
			name:    "fromDate позже toDate",
			period:  wordstat.PeriodDaily,
			from:    "2026-05-10",
			to:      "2026-05-01",
			wantErr: "is after toDate",
		},
		{
			name:    "неизвестный период",
			period:  "PERIOD_UNSPECIFIED",
			wantErr: "unsupported period",
		},
		{
			name:    "битая дата",
			period:  wordstat.PeriodMonthly,
			from:    "вчера",
			wantErr: "invalid date",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to, err := wordstat.ResolveDynamicsRange(tt.period, tt.from, tt.to, now)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got from=%s to=%s", tt.wantErr, from, to)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if from != tt.wantFrom {
				t.Errorf("fromDate = %s, want %s", from, tt.wantFrom)
			}
			if to != tt.wantTo {
				t.Errorf("toDate = %s, want %s", to, tt.wantTo)
			}
		})
	}
}

func TestResolveDynamicsRangeWeeklyFromIsMonday(t *testing.T) {
	// Проверяем инвариант для всех дней года: дефолтный weekly-диапазон
	// всегда начинается с понедельника и заканчивается воскресеньем.
	for day := 0; day < 366; day++ {
		now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC).AddDate(0, 0, day)

		from, to, err := wordstat.ResolveDynamicsRange(wordstat.PeriodWeekly, "", "", now)
		if err != nil {
			t.Fatalf("now=%s: unexpected error: %v", now.Format(dateLayout), err)
		}
		fromTime, err := time.Parse(time.RFC3339, from)
		if err != nil {
			t.Fatalf("parse from: %v", err)
		}
		toTime, err := time.Parse(time.RFC3339, to)
		if err != nil {
			t.Fatalf("parse to: %v", err)
		}
		if fromTime.Weekday() != time.Monday {
			t.Fatalf("now=%s: fromDate %s is %s, want Monday", now.Format(dateLayout), from, fromTime.Weekday())
		}
		if toTime.Weekday() != time.Sunday {
			t.Fatalf("now=%s: toDate %s is %s, want Sunday", now.Format(dateLayout), to, toTime.Weekday())
		}
		if !toTime.Before(now.AddDate(0, 0, 1)) {
			t.Fatalf("now=%s: toDate %s is in the future", now.Format(dateLayout), to)
		}
	}
}

func TestNormalizeDevices(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr string
	}{
		{
			name: "пустой список — валидные «все устройства»",
			in:   nil,
			want: nil,
		},
		{
			name: "регистр не важен, дубли убираются, порядок сохраняется",
			in:   []string{"Phone", "desktop", "phone", " DESKTOP "},
			want: []string{wordstat.DevicePhone, wordstat.DeviceDesktop},
		},
		{
			name: "готовые DEVICE_* принимаются",
			in:   []string{"DEVICE_TABLET", "DEVICE_ALL"},
			want: []string{wordstat.DeviceTablet, wordstat.DeviceAll},
		},
		{
			name:    "неизвестное устройство",
			in:      []string{"watch"},
			wantErr: `invalid device "watch": allowed all, desktop, phone, tablet`,
		},
		{
			name:    "больше трёх устройств",
			in:      []string{"all", "desktop", "phone", "tablet"},
			wantErr: "too many devices: 4, allowed at most 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := wordstat.NormalizeDevices(tt.in)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("NormalizeDevices(%v) = %v, want error", tt.in, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				if !errors.Is(err, wordstat.ErrInvalidArgument) {
					t.Errorf("errors.Is(err, ErrInvalidArgument) = false, err = %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("NormalizeDevices(%v) unexpected error: %v", tt.in, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("NormalizeDevices(%v) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("NormalizeDevices(%v) = %v, want %v", tt.in, got, tt.want)
				}
			}
		})
	}
}

func TestValidateRegions(t *testing.T) {
	tooMany := make([]string, 0, wordstat.MaxRegions+1)
	for i := 0; i <= wordstat.MaxRegions; i++ {
		tooMany = append(tooMany, strconv.Itoa(1000+i))
	}

	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr string
	}{
		{
			name: "пустой список — валидная «вся Россия»",
			in:   nil,
			want: nil,
		},
		{
			name: "дубли и пробелы нормализуются",
			in:   []string{"213", " 1 ", "213", "225"},
			want: []string{"213", "1", "225"},
		},
		{
			name:    "нецифровой регион",
			in:      []string{"abc"},
			wantErr: `invalid region "abc": expected numeric geo id`,
		},
		{
			name:    "пустой регион",
			in:      []string{""},
			wantErr: `invalid region "": expected numeric geo id`,
		},
		{
			name:    "регион с буквами и цифрами",
			in:      []string{"213a"},
			wantErr: `invalid region "213a": expected numeric geo id`,
		},
		{
			name:    "больше лимита proto",
			in:      tooMany,
			wantErr: "too many regions: 101, allowed at most 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := wordstat.ValidateRegions(tt.in)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ValidateRegions(%v) = %v, want error", tt.in, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				if !errors.Is(err, wordstat.ErrInvalidArgument) {
					t.Errorf("errors.Is(err, ErrInvalidArgument) = false, err = %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("ValidateRegions(%v) unexpected error: %v", tt.in, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ValidateRegions(%v) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ValidateRegions(%v) = %v, want %v", tt.in, got, tt.want)
				}
			}
		})
	}
}
