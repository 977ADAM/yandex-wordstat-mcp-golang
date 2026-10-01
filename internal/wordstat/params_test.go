package wordstat

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizePeriod(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: PeriodMonthly},
		{in: "monthly", want: PeriodMonthly},
		{in: "MONTHLY", want: PeriodMonthly},
		{in: " PERIOD_MONTHLY ", want: PeriodMonthly},
		{in: "daily", want: PeriodDaily},
		{in: "DAY", want: PeriodDaily},
		{in: "Period_Daily", want: PeriodDaily},
		{in: "weekly", want: PeriodWeekly},
		{in: "week", want: PeriodWeekly},
		{in: "yearly", wantErr: true},
		{in: "PERIOD_UNSPECIFIED", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := normalizePeriod(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizePeriod(%q) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizePeriod(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("normalizePeriod(%q) = %q, want %q", tt.in, got, tt.want)
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
		{in: "", want: RegionAll},
		{in: "all", want: RegionAll},
		{in: "REGION_ALL", want: RegionAll},
		{in: "cities", want: RegionCities},
		{in: "City", want: RegionCities},
		{in: "regions", want: RegionRegions},
		{in: "REGION_REGIONS", want: RegionRegions},
		{in: "districts", wantErr: true},
		{in: "REGION_UNSPECIFIED", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := normalizeRegion(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeRegion(%q) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeRegion(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("normalizeRegion(%q) = %q, want %q", tt.in, got, tt.want)
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
			got, err := parseDate(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseDate(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDate(%q) unexpected error: %v", tt.in, err)
			}
			if s := got.Format(time.RFC3339); s != tt.want {
				t.Errorf("parseDate(%q) = %s, want %s", tt.in, s, tt.want)
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
			period:   PeriodMonthly,
			wantFrom: "2025-10-01T00:00:00Z",
			wantTo:   "2026-09-30T00:00:00Z",
		},
		{
			name:     "monthly: границы не меняются",
			period:   PeriodMonthly,
			from:     "2026-01-01T00:00:00Z",
			to:       "2026-03-31T00:00:00Z",
			wantFrom: "2026-01-01T00:00:00Z",
			wantTo:   "2026-03-31T00:00:00Z",
		},
		{
			name:     "monthly: только toDate — from считается от него",
			period:   PeriodMonthly,
			to:       "2026-06-30",
			wantFrom: "2025-07-01T00:00:00Z",
			wantTo:   "2026-06-30T00:00:00Z",
		},
		{
			name:    "monthly: fromDate не первое число",
			period:  PeriodMonthly,
			from:    "2026-01-15",
			wantErr: "must be the first day of a month",
		},
		{
			name:    "monthly: toDate не последнее число",
			period:  PeriodMonthly,
			from:    "2026-01-01",
			to:      "2026-03-15",
			wantErr: "must be the last day of a month",
		},
		{
			name:     "weekly: дефолт — от понедельника до последнего воскресенья",
			period:   PeriodWeekly,
			wantFrom: "2026-07-06T00:00:00Z",
			wantTo:   "2026-09-27T00:00:00Z",
		},
		{
			name:     "weekly: только toDate — from считается от него",
			period:   PeriodWeekly,
			to:       "2026-06-28",
			wantFrom: "2026-04-06T00:00:00Z",
			wantTo:   "2026-06-28T00:00:00Z",
		},
		{
			name:    "weekly: fromDate не понедельник",
			period:  PeriodWeekly,
			from:    "2026-03-03",
			wantErr: "must be a Monday",
		},
		{
			name:    "weekly: toDate не воскресенье",
			period:  PeriodWeekly,
			from:    "2026-03-02",
			to:      "2026-04-04",
			wantErr: "must be a Sunday",
		},
		{
			name:     "daily: дефолт — последние 60 дней",
			period:   PeriodDaily,
			wantFrom: "2026-08-02T00:00:00Z",
			wantTo:   "2026-09-30T00:00:00Z",
		},
		{
			name:     "daily: только toDate",
			period:   PeriodDaily,
			to:       "2026-05-10",
			wantFrom: "2026-03-12T00:00:00Z",
			wantTo:   "2026-05-10T00:00:00Z",
		},
		{
			name:    "fromDate позже toDate",
			period:  PeriodDaily,
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
			period:  PeriodMonthly,
			from:    "вчера",
			wantErr: "invalid date",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to, err := resolveDynamicsRange(tt.period, tt.from, tt.to, now)

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

		from, to, err := resolveDynamicsRange(PeriodWeekly, "", "", now)
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
