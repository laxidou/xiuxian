package biz

import (
	"context"
	"testing"
	"time"
)

func TestInitialWorldCanPlaceDeathOpportunityAwayFromDeath(t *testing.T) {
	clock := NewManualClock(time.UnixMilli(1_700_000_000_000))
	service := NewService(clock)
	_, state, err := service.Register(context.Background(), "origin", "a sufficiently long password", "原点真人")
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(8 * time.Hour)
	if _, err := service.State(context.Background(), state.ID); err != nil {
		t.Fatal(err)
	}
	if len(service.opportunities) != 1 {
		t.Fatalf("opportunities = %d, want 1", len(service.opportunities))
	}
	for _, opportunity := range service.opportunities {
		if opportunity.Status != OpportunityUnclaimed {
			t.Fatalf("status = %s, want %s", opportunity.Status, OpportunityUnclaimed)
		}
		if opportunity.Position == opportunity.DeathPosition {
			t.Fatalf("opportunity remained at death position %#v", opportunity.Position)
		}
	}
}
