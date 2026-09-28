package web

// Builds the profile's "rhythm" section: a year of daily plays as a heatmap, plays by hour of
// day as a radial clock, and streaks. Everything is precomputed here so the template only
// draws cells and SVG lines.

import (
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"muzi/db"
)

type HeatCell struct {
	Date   time.Time
	Count  int
	Level  int  // 0 (no plays) to 4 (busiest quarter of active days)
	Future bool // after today; drawn empty
}

type HeatWeek struct {
	Days       []HeatCell
	MonthLabel string // set on the first week of each month
}

type ClockBar struct {
	X1, Y1, X2, Y2 float64
	Hour           int
	Count          int
	Peak           bool
}

type Rhythm struct {
	Weeks         []HeatWeek
	Clock         []ClockBar
	PeakHour      string
	YearTotal     int
	ActiveDays    int
	CurrentStreak int
	LongestStreak int
	BusiestDay    time.Time
	BusiestCount  int
}

// Clock geometry, in the template's 200x200 SVG viewBox
const (
	clockCenter   = 100.0
	clockInner    = 30.0
	clockMaxBar   = 58.0
	clockMinBar   = 3.0
	heatmapWeeks  = 53
	daysPerWeek   = 7
	heatmapLevels = 4
)

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// Builds the section in the user's timezone (tz is its IANA name, "" for the server's)
func buildRhythm(userId int, loc *time.Location, tz string) *Rhythm {
	today := startOfDay(time.Now().In(loc))
	// weeks run Sunday to Saturday, with the current week last
	start := today.AddDate(0, 0, -int(today.Weekday())-(heatmapWeeks-1)*daysPerWeek)

	hourly, err := db.GetHourlyPlayCounts(userId, start, tz)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot get hourly play counts: %v\n", err)
		return nil
	}

	daily := make(map[time.Time]int)
	var byHour [24]int
	r := &Rhythm{}
	for hour, count := range hourly {
		local := hour.In(loc)
		if tz != "" {
			// already bucketed in the user's zone; keys are wall-clock times
			local = time.Date(hour.Year(), hour.Month(), hour.Day(), hour.Hour(), 0, 0, 0, loc)
		}
		daily[startOfDay(local)] += count
		byHour[local.Hour()] += count
		r.YearTotal += count
	}

	thresholds := levelThresholds(daily)
	for w := 0; w < heatmapWeeks; w++ {
		var week HeatWeek
		for d := 0; d < daysPerWeek; d++ {
			day := start.AddDate(0, 0, w*daysPerWeek+d)
			cell := HeatCell{Date: day, Count: daily[day], Future: day.After(today)}
			for cell.Count > 0 && cell.Level < heatmapLevels && cell.Count >= thresholds[cell.Level] {
				cell.Level++
			}
			if day.Day() == 1 || (w == 0 && d == 0) {
				week.MonthLabel = day.Format("Jan")
			}
			week.Days = append(week.Days, cell)

			if cell.Count > r.BusiestCount {
				r.BusiestCount, r.BusiestDay = cell.Count, day
			}
		}
		r.Weeks = append(r.Weeks, week)
	}
	// the first column's label would crowd the next month's when it starts mid-month
	if len(r.Weeks) > 1 && r.Weeks[1].MonthLabel != "" {
		r.Weeks[0].MonthLabel = ""
	}

	r.ActiveDays = len(daily)
	r.CurrentStreak, r.LongestStreak = streaks(daily, start, today)
	r.Clock, r.PeakHour = buildClock(byHour)
	return r
}

// Splits active days' counts into quartiles, so one huge day doesn't wash out the rest
func levelThresholds(daily map[time.Time]int) [heatmapLevels]int {
	var counts []int
	for _, c := range daily {
		if c > 0 {
			counts = append(counts, c)
		}
	}
	sort.Ints(counts)
	var t [heatmapLevels]int
	for i := range t {
		if len(counts) == 0 {
			t[i] = math.MaxInt
			continue
		}
		t[i] = counts[len(counts)*i/heatmapLevels]
	}
	return t
}

// Current streak counts back from today (or yesterday, if nothing's been played yet today)
func streaks(daily map[time.Time]int, start, today time.Time) (current, longest int) {
	run := 0
	for day := start; !day.After(today); day = day.AddDate(0, 0, 1) {
		if daily[day] > 0 {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}

	day := today
	if daily[day] == 0 {
		day = day.AddDate(0, 0, -1)
	}
	for daily[day] > 0 {
		current++
		day = day.AddDate(0, 0, -1)
	}
	return current, longest
}

func buildClock(byHour [24]int) ([]ClockBar, string) {
	peak := 0
	for h, c := range byHour {
		if c > byHour[peak] {
			peak = h
		}
	}

	bars := make([]ClockBar, 24)
	for h, c := range byHour {
		length := clockMinBar
		if byHour[peak] > 0 {
			length += (clockMaxBar - clockMinBar) * float64(c) / float64(byHour[peak])
		}
		// midnight at the top, running clockwise; each bar sits in the middle of its hour
		angle := (float64(h)+0.5)/24*2*math.Pi - math.Pi/2
		cos, sin := math.Cos(angle), math.Sin(angle)
		bars[h] = ClockBar{
			X1:    round1(clockCenter + clockInner*cos),
			Y1:    round1(clockCenter + clockInner*sin),
			X2:    round1(clockCenter + (clockInner+length)*cos),
			Y2:    round1(clockCenter + (clockInner+length)*sin),
			Hour:  h,
			Count: c,
			Peak:  h == peak && c > 0,
		}
	}
	if byHour[peak] == 0 {
		return bars, ""
	}
	return bars, hourLabel(peak)
}

func hourLabel(h int) string {
	return time.Date(2000, 1, 1, h, 0, 0, 0, time.Local).Format("3pm")
}

func round1(f float64) float64 {
	return math.Round(f*10) / 10
}
