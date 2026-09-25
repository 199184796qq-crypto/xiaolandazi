package timeline

import (
	"testing"
	"time"
)

func TestPinsCompactOutsideDynamicHotWindow(t *testing.T) {
	now := time.Now().UTC()
	s := NewSession(now.Add(-time.Hour))
	s.Append(Pin{At: now.Add(-6 * time.Minute), Kind: PinCTA, Strategy: "conversion", Topic: "PRICE"})
	s.Append(Pin{At: now.Add(-2 * time.Minute), Kind: PinHumor, Strategy: "humor"})
	view := s.View(now, HeatBusy)
	if len(view.HotPins) != 1 || view.HotPins[0].Kind != PinHumor {
		t.Fatalf("hot=%#v", view.HotPins)
	}
	if view.Summary.CountsByKind[PinCTA] != 1 {
		t.Fatalf("summary=%#v", view.Summary)
	}
	if view.HotWindow != 5*time.Minute {
		t.Fatalf("window=%v", view.HotWindow)
	}
}

func TestHotAndColdWindowsDiffer(t *testing.T) {
	if HotWindowForHeat(HeatHot) != 3*time.Minute {
		t.Fatal("hot window")
	}
	if HotWindowForHeat(HeatCold) != 10*time.Minute {
		t.Fatal("cold window")
	}
}

func TestStrategyDebtCooldown(t *testing.T) {
	now := time.Now().UTC()
	s := NewSession(now)
	state := s.RaiseDebt(DebtLikeCTA, .8, now)
	if state.Value != .8 {
		t.Fatalf("state=%#v", state)
	}
	s.SpendDebt(DebtLikeCTA, 5*time.Minute, now)
	state = s.RaiseDebt(DebtLikeCTA, 1, now.Add(time.Minute))
	if state.Value != 0 {
		t.Fatalf("cooldown should block raise: %#v", state)
	}
	state = s.RaiseDebt(DebtLikeCTA, 1, now.Add(6*time.Minute))
	if state.Value != 1 {
		t.Fatalf("debt should reopen: %#v", state)
	}
}
