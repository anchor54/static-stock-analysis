package indicators

import (
	"fmt"
	"math"
	"strings"
)

// StageConfig holds the configurable Weinstein stage thresholds.
type StageConfig struct {
	MAWeeks    int     // length of the weekly SMA (30)
	SlopeWeeks int     // slope = % change of the MA over this many weeks (4)
	FlatPct    float64 // a slope within ±FlatPct counts as flat (1)
	BandPct    float64 // pullback / bounce band around the MA, in % (5)
	EarlyWeeks int     // "early" if the MA trend differed this many weeks ago (8)
	PriorWeeks int     // look-back for a flat MA's prior trend, ~6 months (26)
}

// DefaultStageConfig returns the thresholds from the design doc.
func DefaultStageConfig() StageConfig {
	return StageConfig{MAWeeks: 30, SlopeWeeks: 4, FlatPct: 1, BandPct: 5, EarlyWeeks: 8, PriorWeeks: 26}
}

// MinWeeks is the number of weekly bars needed to classify a stage.
func (c StageConfig) MinWeeks() int { return c.MAWeeks + c.SlopeWeeks }

// Stage is a Weinstein stage classification with its reasoning.
type Stage struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

var stageNames = map[int]string{1: "Basing", 2: "Advancing", 3: "Topping", 4: "Declining"}

type trend int

const (
	unknown trend = iota
	rising
	falling
	flat
)

func (t trend) String() string {
	switch t {
	case rising:
		return "rising"
	case falling:
		return "falling"
	case flat:
		return "flat"
	}
	return "unknown"
}

// WeinsteinStage classifies the latest weekly close against its MAWeeks SMA
// and that SMA's slope. rs55 adds a caution to Stage 2 when below 0 (NaN is
// ignored). ok is false when there is too little history.
func WeinsteinStage(weeklyCloses []float64, rs55 float64, cfg StageConfig) (Stage, bool) {
	n := len(weeklyCloses)
	if cfg.MAWeeks <= 0 || cfg.SlopeWeeks <= 0 || n < cfg.MinWeeks() {
		return Stage{}, false
	}
	ma := SMASeries(weeklyCloses, cfg.MAWeeks)
	t := n - 1

	slopeAt := func(i int) float64 {
		j := i - cfg.SlopeWeeks
		if i < 0 || j < 0 || math.IsNaN(ma[i]) || math.IsNaN(ma[j]) || ma[j] == 0 {
			return math.NaN()
		}
		return (ma[i]/ma[j] - 1) * 100
	}
	trendOf := func(slope float64) trend {
		switch {
		case math.IsNaN(slope):
			return unknown
		case slope > cfg.FlatPct:
			return rising
		case slope < -cfg.FlatPct:
			return falling
		}
		return flat
	}

	slope := slopeAt(t)
	tr := trendOf(slope)
	price := weeklyCloses[t]
	dist := PctDist(price, ma[t])
	prevTrend := unknown
	if cfg.EarlyWeeks > 0 {
		prevTrend = trendOf(slopeAt(t - cfg.EarlyWeeks))
	}

	where := fmt.Sprintf("%.1f%% above", dist)
	if dist < 0 {
		where = fmt.Sprintf("%.1f%% below", -dist)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d-week MA is %s (%+.1f%% over %d weeks); price is %s it.",
		cfg.MAWeeks, tr, slope, cfg.SlopeWeeks, where)

	var num int
	var qual string
	switch tr {
	case rising:
		switch {
		case dist >= 0:
			num = 2
			if prevTrend != unknown && prevTrend != rising {
				qual = "early"
				fmt.Fprintf(&b, " The MA was %s %d weeks ago, so the advance is early.", prevTrend, cfg.EarlyWeeks)
			}
		case dist >= -cfg.BandPct:
			num, qual = 2, "pullback"
			fmt.Fprintf(&b, " Within %.0f%% below a rising MA: a pullback.", cfg.BandPct)
		default:
			num = 3
			fmt.Fprintf(&b, " More than %.0f%% below a still-rising MA: topping.", cfg.BandPct)
		}
	case falling:
		switch {
		case dist < 0:
			num = 4
			if prevTrend != unknown && prevTrend != falling {
				qual = "early"
				fmt.Fprintf(&b, " The MA was %s %d weeks ago, so the decline is early.", prevTrend, cfg.EarlyWeeks)
			}
		case dist <= cfg.BandPct:
			num, qual = 4, "bounce"
			fmt.Fprintf(&b, " Within %.0f%% above a falling MA: a bounce.", cfg.BandPct)
		default:
			num = 1
			fmt.Fprintf(&b, " More than %.0f%% above a still-falling MA: basing.", cfg.BandPct)
		}
	default: // flat
		prior := math.NaN()
		if j := t - cfg.PriorWeeks; cfg.PriorWeeks > 0 && j >= 0 && !math.IsNaN(ma[j]) {
			prior = PctDist(ma[t], ma[j])
		}
		if !math.IsNaN(prior) && prior > cfg.FlatPct {
			num = 3
			fmt.Fprintf(&b, " The MA rose %.1f%% over the prior %d weeks, so a flat MA marks a top.", prior, cfg.PriorWeeks)
		} else {
			num = 1
			if math.IsNaN(prior) {
				fmt.Fprintf(&b, " Not enough history to judge the prior %d-week trend; treated as a base.", cfg.PriorWeeks)
			} else {
				fmt.Fprintf(&b, " The MA did not rise over the prior %d weeks (%+.1f%%), so a flat MA marks a base.", cfg.PriorWeeks, prior)
			}
		}
	}

	if num == 2 && rs55 < 0 {
		fmt.Fprintf(&b, " Caution: RS55 is %.3f, lagging the Nifty 50.", rs55)
	}

	label := stageNames[num]
	if qual != "" {
		label += " (" + qual + ")"
	}
	return Stage{Number: num, Name: stageNames[num], Label: label, Reason: b.String()}, true
}
