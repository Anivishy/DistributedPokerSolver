package cfr

import (
	"sort"

	"poker-solver/pkg/deck"
	"poker-solver/pkg/game"
	"poker-solver/pkg/util"
)

type RangeTracker struct {
	weights []float64
	all     []deck.Combo
}

func NewRangeTracker() *RangeTracker {
	all := deck.All1326()
	weights := make([]float64, len(all))
	for i := range weights {
		weights[i] = 1.0
	}
	return &RangeTracker{weights: weights, all: all}
}

func (rt *RangeTracker) Clone() *RangeTracker {
	w := make([]float64, len(rt.weights))
	copy(w, rt.weights)
	return &RangeTracker{weights: w, all: rt.all}
}

func (rt *RangeTracker) Weights() []float64 {
	return rt.weights
}

func (rt *RangeTracker) BlockCards(blocked []deck.Card) {
	for i, combo := range rt.all {
		if combo.BlockedBy(blocked) {
			rt.weights[i] = 0
		}
	}
	rt.normalize()
}

func (rt *RangeTracker) ObserveAction(actionType game.ActionType, board []deck.Card) {
	for i, combo := range rt.all {
		if rt.weights[i] == 0 {
			continue
		}
		eq := game.Equity([]deck.Card{combo.C1, combo.C2}, board, 10)
		likelihood := util.ActionLikelihood(actionType, eq)
		rt.weights[i] *= likelihood
	}
	rt.normalize()
}

func (rt *RangeTracker) normalize() {
	total := 0.0
	for _, w := range rt.weights {
		total += w
	}
	if total <= 0 {
		for i := range rt.weights {
			rt.weights[i] = 1.0 / float64(len(rt.weights))
		}
		return
	}
	for i := range rt.weights {
		rt.weights[i] /= total
	}
}

type RangeStats struct {
	AverageEquity float64       `json:"average_equity"`
	TotalWeight   float64       `json:"total_weight"`
	NumActive     int           `json:"num_active"`
	TopCombos     []ComboWeight `json:"top_combos"`
}

type ComboWeight struct {
	Combo  string  `json:"combo"`
	Weight float64 `json:"weight"`
	Equity float64 `json:"equity"`
}

func (rt *RangeTracker) Stats(board []deck.Card) RangeStats {
	totalWeight := 0.0
	totalEquity := 0.0
	numActive := 0

	for i, w := range rt.weights {
		if w <= 0 {
			continue
		}
		combo := rt.all[i]
		eq := game.Equity([]deck.Card{combo.C1, combo.C2}, board, 10)
		totalEquity += eq * w
		totalWeight += w
		numActive++
	}

	avgEq := 0.5
	if totalWeight > 0 {
		avgEq = totalEquity / totalWeight
	}

	return RangeStats{
		AverageEquity: avgEq,
		TotalWeight:   totalWeight,
		NumActive:     numActive,
		TopCombos:     rt.TopCombos(20, board),
	}
}

func (rt *RangeTracker) TopCombos(n int, board []deck.Card) []ComboWeight {
	type indexed struct {
		idx int
		w   float64
	}
	var active []indexed
	for i, w := range rt.weights {
		if w > 0 {
			active = append(active, indexed{i, w})
		}
	}
	sort.Slice(active, func(a, b int) bool {
		return active[a].w > active[b].w
	})
	if len(active) > n {
		active = active[:n]
	}
	result := make([]ComboWeight, len(active))
	for i, a := range active {
		combo := rt.all[a.idx]
		eq := game.Equity([]deck.Card{combo.C1, combo.C2}, board, 10)
		result[i] = ComboWeight{
			Combo:  combo.C1.String() + combo.C2.String(),
			Weight: a.w,
			Equity: eq,
		}
	}
	return result
}
