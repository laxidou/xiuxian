package rules_test

import (
	"testing"
	"time"

	"xiuxian/internal/rules"
)

func TestRealmLookupUsesPublishedWorkedValues(t *testing.T) {
	tests := []struct {
		cultivation rules.Cultivation
		wantLevel   int
		wantName    string
		wantSpeed   int64
		wantSense   int64
	}{
		{rules.Points(0), 0, "凡人", 1, 1},
		{rules.Points(5), 3, "炼气后期", 5, 25},
		{rules.Points(377), 12, "元婴后期", 377, 142129},
		{rules.Points(610), 13, "化神初期", 610, 372100},
		{rules.Points(3524578), 31, "仙尊", 3524578, 12422650078084},
	}

	for _, tt := range tests {
		realm := rules.RealmFor(tt.cultivation)
		if realm.Level != tt.wantLevel || realm.Name != tt.wantName || realm.Speed != tt.wantSpeed || realm.SenseRadius != tt.wantSense {
			t.Fatalf("RealmFor(%v) = %+v, want level=%d name=%q speed=%d sense=%d", tt.cultivation, realm, tt.wantLevel, tt.wantName, tt.wantSpeed, tt.wantSense)
		}
	}
}

func TestBreakthroughIsDerivedBeforeLifespanAtSameMillisecond(t *testing.T) {
	state := rules.DeriveLife(rules.Points(2), 60*time.Minute)
	if !state.Alive {
		t.Fatal("role should survive because cultivation reaches 炼气初期 before the lifespan check")
	}
	if state.Realm.Level != 1 {
		t.Fatalf("realm = %d, want 1", state.Realm.Level)
	}
}

func TestNaturalDeathUsesGreaterThanOrEqualBoundary(t *testing.T) {
	state := rules.DeriveLife(rules.Points(1), 60*time.Minute)
	if state.Alive {
		t.Fatal("凡人 should die exactly at one hour")
	}
}

func TestNextNaturalDeathSkipsReachableBreakthroughs(t *testing.T) {
	got := rules.NextNaturalDeathAfter(0, 0)
	if got != 8*time.Hour {
		t.Fatalf("natural death after = %v, want 8h (元婴后期 lifespan ceiling)", got)
	}
}

func TestNextNaturalDeathTreatsEqualThresholdAsBreakthroughFirst(t *testing.T) {
	// A synthetic state one millisecond before the 炼气 threshold and the
	// current lifespan boundary reaches both at once. The new realm extends life.
	got := rules.NextNaturalDeathAfter(rules.Points(2)-1, time.Hour-time.Millisecond)
	if got != 7*time.Hour+time.Millisecond {
		t.Fatalf("remaining life = %v, want 7h+1ms after breakthrough", got)
	}
}

func TestTransferRejectsOnlyWhenPostTransferLifespanIsBelowAge(t *testing.T) {
	tooOld := rules.ValidateTransfer(rules.Points(34), rules.Points(26), 4*time.Hour)
	if tooOld.Allowed || tooOld.DeathAfterTransfer {
		t.Fatalf("4-hour-old role dropping to 筑基 should be rejected: %+v", tooOld)
	}

	critical := rules.ValidateTransfer(rules.Points(34), rules.Points(26), 3*time.Hour)
	if !critical.Allowed || !critical.DeathAfterTransfer {
		t.Fatalf("3-hour-old role dropping to 筑基 should transfer then die: %+v", critical)
	}
}

func TestTrajectoryUsesCanonicalCoordinatesAndReachesExactTarget(t *testing.T) {
	trajectory := rules.Trajectory{
		Start:     rules.Position{X: rules.Units(0), Y: rules.Units(0)},
		Target:    rules.Position{X: rules.Units(3), Y: rules.Units(4)},
		StartedAt: time.UnixMilli(1_000),
		Speed:     1,
	}

	mid, arrived := trajectory.PositionAt(time.UnixMilli(3_500))
	if arrived || mid != (rules.Position{X: rules.Units(1.5), Y: rules.Units(2)}) {
		t.Fatalf("midpoint = %+v arrived=%v, want (1.5,2) false", mid, arrived)
	}

	end, arrived := trajectory.PositionAt(time.UnixMilli(6_000))
	if !arrived || end != trajectory.Target {
		t.Fatalf("end = %+v arrived=%v, want exact target %+v true", end, arrived, trajectory.Target)
	}
}

func TestNaturalTravelDistanceUsesNewSpeedAfterBreakthrough(t *testing.T) {
	// One second at 凡人 speed reaches the threshold, then one second at
	// 炼气初期 speed: 1*1 + 1*2 = 3 world units.
	got := rules.NaturalTravelDistance(rules.Points(2)-rules.Cultivation(time.Second/time.Millisecond), 2*time.Second)
	if got != 3 {
		t.Fatalf("travel distance = %v, want 3", got)
	}
}

func TestDirectionalTrajectoryMovesForeverAlongOneCardinalAxis(t *testing.T) {
	start := rules.Position{X: rules.Units(2), Y: rules.Units(-1)}
	tests := []struct {
		direction rules.Direction
		want      rules.Position
	}{
		{rules.DirectionUp, rules.Position{X: rules.Units(2), Y: rules.Units(2.5)}},
		{rules.DirectionDown, rules.Position{X: rules.Units(2), Y: rules.Units(-4.5)}},
		{rules.DirectionLeft, rules.Position{X: rules.Units(-1.5), Y: rules.Units(-1)}},
		{rules.DirectionRight, rules.Position{X: rules.Units(5.5), Y: rules.Units(-1)}},
	}
	for _, test := range tests {
		trajectory := rules.Trajectory{Mode: rules.TrajectoryDirection, Start: start, Direction: test.direction}
		position, arrived := trajectory.PositionAfterDistance(3.5)
		if arrived || position != test.want {
			t.Fatalf("direction %s position = %+v arrived=%v, want %+v false", test.direction, position, arrived, test.want)
		}
	}
}

func TestTravelDistanceUsesDesiredSpeedAsRealmBoundedSetting(t *testing.T) {
	start := rules.Points(2) - rules.Cultivation(time.Second/time.Millisecond)
	if got := rules.TravelDistance(start, 2*time.Second, 1); got != 2 {
		t.Fatalf("desired-speed distance = %v, want 2", got)
	}
	if got := rules.TravelDistance(start, 2*time.Second, 10); got != 3 {
		t.Fatalf("realm-bounded distance = %v, want 3", got)
	}
	if got := rules.TravelDistance(start, 2*time.Second, 2); got != 3 {
		t.Fatalf("desired speed after breakthrough = %v, want 3", got)
	}
}

func TestOpportunitySenseUsesCombinedRadius(t *testing.T) {
	role := rules.Position{X: rules.Units(0), Y: rules.Units(0)}
	opportunity := rules.Position{X: rules.Units(8), Y: rules.Units(0)}
	if !rules.CanSenseOpportunity(role, rules.Units(5), opportunity, rules.Units(3)) {
		t.Fatal("touching sense circles should count as 感应到机缘")
	}
}

func TestOpportunityConversionIsLinearAcrossTwentyFourHours(t *testing.T) {
	total := rules.Points(240)
	if got := rules.ConvertedCultivation(total, 6*time.Hour); got != rules.Points(60) {
		t.Fatalf("converted after 6h = %v, want 60 points", got)
	}
	if got := rules.ConvertedCultivation(total, 24*time.Hour); got != total {
		t.Fatalf("converted after 24h = %v, want total %v", got, total)
	}
	if got := rules.ConvertedCultivation(total, 48*time.Hour); got != total {
		t.Fatalf("converted after 48h = %v, want capped total %v", got, total)
	}
}
