package domain

import (
	"math"
	"testing"
	"time"
)

// TestWalkingTime checks a line is walked at the speed on the flat where it is
// flat or its slopes are unknown, faster down a gentle descent and slower up a
// climb, as the browser works it out.
func TestWalkingTime(t *testing.T) {
	flat := Track{DistanceM: 4700}
	if got := flat.WalkingTime(4.7); got.Round(time.Second) != time.Hour {
		t.Errorf("flat: %v", got)
	}

	grades := make([]int, 101)
	grades[45] = 4700 // -5 %, Tobler's fastest
	descent := time.Duration(float64(time.Hour) / math.Exp(3.5*0.05))
	if got := (Track{DistanceM: 4700, Grades: grades}).WalkingTime(4.7); got.Round(time.Second) != descent.Round(time.Second) {
		t.Errorf("gentle descent: %v, want %v", got, descent)
	}

	grades = make([]int, 101)
	grades[60] = 4700 // +10 %
	want := time.Duration(float64(time.Hour) / math.Exp(-3.5*0.1))
	if got := (Track{DistanceM: 4700, Grades: grades}).WalkingTime(4.7); got.Round(time.Second) != want.Round(time.Second) {
		t.Errorf("climb: %v, want %v", got, want)
	}

	if got := flat.WalkingTime(0); got != 0 {
		t.Errorf("no speed: %v", got)
	}
}
