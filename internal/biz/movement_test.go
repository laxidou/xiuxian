package biz

import (
	"context"
	"testing"
	"time"

	"xiuxian/internal/rules"
)

func TestDirectionalSpeedClampsAfterRealmLossAndRecoversToSetting(t *testing.T) {
	clock := NewManualClock(time.UnixMilli(1_700_000_000_000))
	service := NewService(clock)
	_, sender, err := service.Register(context.Background(), "direction-sender", "a sufficiently long password", "行者")
	if err != nil {
		t.Fatal(err)
	}
	_, recipient, err := service.Register(context.Background(), "direction-recipient", "a sufficiently long password", "受者")
	if err != nil {
		t.Fatal(err)
	}

	clock.Advance(5 * time.Minute)
	sender, err = service.State(context.Background(), sender.ID)
	if err != nil {
		t.Fatal(err)
	}
	moving, err := service.MoveDirection(context.Background(), sender.ID, "direction-at-five", rules.DirectionRight, 5, CommandExpectation{LifeNumber: sender.LifeNumber, StateVersion: sender.StateVersion})
	if err != nil {
		t.Fatal(err)
	}
	clamped, err := service.Transfer(context.Background(), sender.ID, recipient.ID, "direction-realm-loss", 4, CommandExpectation{LifeNumber: moving.LifeNumber, StateVersion: moving.StateVersion})
	if err != nil {
		t.Fatal(err)
	}
	if clamped.MovementSpeedSetting != 5 || clamped.ActualMovementSpeed != 1 {
		t.Fatalf("speed after realm loss = setting %d actual %d, want 5 and 1", clamped.MovementSpeedSetting, clamped.ActualMovementSpeed)
	}

	clock.Advance(time.Minute)
	recovered, err := service.State(context.Background(), sender.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.MovementSpeedSetting != 5 || recovered.ActualMovementSpeed != 2 || recovered.Position.X != 60 {
		t.Fatalf("recovered direction state = %#v, want setting 5, actual 2, position x=60", recovered)
	}
}
