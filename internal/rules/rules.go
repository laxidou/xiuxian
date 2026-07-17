// Package rules contains the deterministic, side-effect-free rules of the
// continuous cultivation world. Cultivation is represented as milliseconds of
// natural cultivation time (60,000 units == one cultivation point), while
// coordinates use thousandths of one world unit.
package rules

import (
	"math"
	"sort"
	"time"
)

const (
	Version                  = int32(1)
	cultivationUnitsPerPoint = int64(time.Minute / time.Millisecond)
	coordinateScale          = int64(1000)
	opportunityDuration      = 24 * time.Hour
)

type Cultivation int64

func Points(value float64) Cultivation {
	return Cultivation(math.Round(value * float64(cultivationUnitsPerPoint)))
}

func (c Cultivation) Points() float64 {
	return float64(c) / float64(cultivationUnitsPerPoint)
}

type Coordinate int64

func Units(value float64) Coordinate {
	return Coordinate(math.Round(value * float64(coordinateScale)))
}

func (c Coordinate) Units() float64 {
	return float64(c) / float64(coordinateScale)
}

type Position struct {
	X Coordinate `json:"x"`
	Y Coordinate `json:"y"`
}

type Realm struct {
	Level       int
	Name        string
	Threshold   Cultivation
	Lifespan    time.Duration
	Speed       int64
	SenseRadius int64
}

var realms = func() []Realm {
	names := []string{
		"凡人", "炼气初期", "炼气中期", "炼气后期", "筑基初期", "筑基中期", "筑基后期",
		"金丹初期", "金丹中期", "金丹后期", "元婴初期", "元婴中期", "元婴后期",
		"化神初期", "化神中期", "化神后期", "合体初期", "合体中期", "合体后期",
		"大乘初期", "大乘中期", "大乘后期", "渡劫期", "人仙", "天仙", "金仙",
		"大罗金仙", "九天玄仙", "罗天上仙", "仙君", "仙帝", "仙尊",
	}
	thresholds := []int64{0, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233, 377, 610, 987, 1597, 2584, 4181, 6765, 10946, 17711, 28657, 46368, 75025, 121393, 196418, 317811, 514229, 832040, 1346269, 2178309, 3524578}
	speeds := []int64{1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233, 377, 610, 987, 1597, 2584, 4181, 6765, 10946, 17711, 28657, 46368, 75025, 121393, 196418, 317811, 514229, 832040, 1346269, 2178309, 3524578}
	lifespansHours := []int64{1, 2, 2, 2, 3, 3, 3, 5, 5, 5, 8, 8, 8, 13, 13, 13, 21, 21, 21, 34, 34, 34, 55, 89, 144, 233, 377, 610, 987, 1597, 2584, 4181}

	result := make([]Realm, len(names))
	for i := range names {
		result[i] = Realm{
			Level:       i,
			Name:        names[i],
			Threshold:   Points(float64(thresholds[i])),
			Lifespan:    time.Duration(lifespansHours[i]) * time.Hour,
			Speed:       speeds[i],
			SenseRadius: speeds[i] * speeds[i],
		}
	}
	return result
}()

func Realms() []Realm {
	return append([]Realm(nil), realms...)
}

func RealmFor(cultivation Cultivation) Realm {
	index := sort.Search(len(realms), func(i int) bool {
		return realms[i].Threshold > cultivation
	})
	if index == 0 {
		return realms[0]
	}
	return realms[index-1]
}

type LifeState struct {
	Cultivation Cultivation
	Age         time.Duration
	Realm       Realm
	Alive       bool
}

func DeriveLife(cultivation Cultivation, age time.Duration) LifeState {
	realm := RealmFor(cultivation)
	return LifeState{
		Cultivation: cultivation,
		Age:         age,
		Realm:       realm,
		Alive:       age < realm.Lifespan,
	}
}

// NextNaturalDeathAfter returns the duration until the first lifespan boundary
// that natural cultivation cannot escape by breaking through. Cultivation and
// age advance by one internal millisecond per elapsed millisecond. When a
// threshold and lifespan boundary coincide, the threshold is applied first.
func NextNaturalDeathAfter(cultivation Cultivation, age time.Duration) time.Duration {
	var elapsed time.Duration
	for {
		realm := RealmFor(cultivation)
		untilDeath := realm.Lifespan - age
		if untilDeath <= 0 {
			return elapsed
		}
		if realm.Level == len(realms)-1 {
			return elapsed + untilDeath
		}
		untilBreakthrough := time.Duration(realms[realm.Level+1].Threshold-cultivation) * time.Millisecond
		if untilBreakthrough > untilDeath {
			return elapsed + untilDeath
		}
		cultivation += Cultivation(untilBreakthrough.Milliseconds())
		age += untilBreakthrough
		elapsed += untilBreakthrough
	}
}

type TransferValidation struct {
	Allowed            bool
	DeathAfterTransfer bool
	PostTransfer       LifeState
}

func ValidateTransfer(current, amount Cultivation, age time.Duration) TransferValidation {
	if amount <= 0 || amount > current {
		return TransferValidation{}
	}
	post := DeriveLife(current-amount, age)
	if age > post.Realm.Lifespan {
		return TransferValidation{PostTransfer: post}
	}
	return TransferValidation{
		Allowed:            true,
		DeathAfterTransfer: age == post.Realm.Lifespan,
		PostTransfer:       post,
	}
}

type Trajectory struct {
	Start     Position
	Target    Position
	StartedAt time.Time
	Speed     int64
}

func (t Trajectory) PositionAt(now time.Time) (Position, bool) {
	if t.Speed <= 0 || !now.After(t.StartedAt) {
		return t.Start, t.Start == t.Target
	}
	return t.PositionAfterDistance(now.Sub(t.StartedAt).Seconds() * float64(t.Speed))
}

func (t Trajectory) PositionAfterDistance(travelledUnits float64) (Position, bool) {
	dx := float64(t.Target.X - t.Start.X)
	dy := float64(t.Target.Y - t.Start.Y)
	distance := math.Hypot(dx, dy)
	if distance == 0 || travelledUnits*float64(coordinateScale) >= distance {
		return t.Target, true
	}
	ratio := travelledUnits * float64(coordinateScale) / distance
	return Position{
		X: t.Start.X + Coordinate(math.Round(dx*ratio)),
		Y: t.Start.Y + Coordinate(math.Round(dy*ratio)),
	}, false
}

func NaturalTravelDistance(cultivationAtStart Cultivation, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}
	remainingMillis := elapsed.Milliseconds()
	cultivation := cultivationAtStart
	var distance float64
	for remainingMillis > 0 {
		realm := RealmFor(cultivation)
		segmentMillis := remainingMillis
		if realm.Level < len(realms)-1 {
			toThreshold := int64(realms[realm.Level+1].Threshold - cultivation)
			if toThreshold < segmentMillis {
				segmentMillis = toThreshold
			}
		}
		distance += float64(realm.Speed) * float64(segmentMillis) / 1000
		cultivation += Cultivation(segmentMillis)
		remainingMillis -= segmentMillis
	}
	return distance
}

func Distance(a, b Position) float64 {
	return math.Hypot(float64(a.X-b.X), float64(a.Y-b.Y)) / float64(coordinateScale)
}

func CanSenseOpportunity(role Position, roleRadius Coordinate, opportunity Position, opportunityRadius Coordinate) bool {
	combined := float64(roleRadius + opportunityRadius)
	return math.Hypot(float64(role.X-opportunity.X), float64(role.Y-opportunity.Y)) <= combined
}

func ConvertedCultivation(total Cultivation, elapsed time.Duration) Cultivation {
	if total <= 0 || elapsed <= 0 {
		return 0
	}
	if elapsed >= opportunityDuration {
		return total
	}
	return Cultivation(int64(total) * elapsed.Milliseconds() / opportunityDuration.Milliseconds())
}
