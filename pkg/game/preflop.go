package game

import (
	"math"
	"sort"
	"sync"

	"poker-solver/pkg/deck"
)

func chenScore(a, b deck.Card) float64 {
	hi, lo := a.Rank, b.Rank
	if lo > hi {
		hi, lo = lo, hi
	}

	highPoints := func(rank int) float64 {
		switch rank {
		case 14:
			return 10
		case 13:
			return 8
		case 12:
			return 7
		case 11:
			return 6
		default:
			return float64(rank) / 2.0
		}
	}

	if hi == lo {
		return math.Max(highPoints(hi)*2, 5)
	}

	score := highPoints(hi)
	if a.Suit == b.Suit {
		score += 2
	}

	gap := hi - lo - 1
	switch {
	case gap == 1:
		score -= 1
	case gap == 2:
		score -= 2
	case gap == 3:
		score -= 4
	case gap >= 4:
		score -= 5
	}

	if gap <= 1 && hi < 12 {
		score += 1
	}

	if score < 0 {
		score = 0
	}
	return score
}

var (
	preflopRankTable []float64
	preflopRankOnce  sync.Once
)

func buildPreflopRankTable() {
	all := deck.All1326()
	scores := make([]float64, len(all))
	for i, c := range all {
		scores[i] = chenScore(c.C1, c.C2)
	}

	sorted := append([]float64(nil), scores...)
	sort.Float64s(sorted)

	n := float64(len(all))
	preflopRankTable = make([]float64, len(all))
	for i, s := range scores {
		below := sort.SearchFloat64s(sorted, s)
		above := sort.SearchFloat64s(sorted, math.Nextafter(s, math.Inf(1)))
		equal := above - below
		preflopRankTable[i] = (float64(below) + 0.5*float64(equal)) / n
	}
}

func PreflopHandRank(combo deck.Combo) float64 {
	preflopRankOnce.Do(buildPreflopRankTable)
	idx := combo.Index()
	if idx < 0 || idx >= len(preflopRankTable) {
		return 0.5
	}
	return preflopRankTable[idx]
}

func openFreq(position int) float64 {
	switch position {
	case 0:
		return 0.14
	case 1:
		return 0.18
	case 2:
		return 0.27
	case 3:
		return 0.45
	case 4:
		return 0.42
	case 5:
		return 0.55
	}
	return 0.30
}

func rangeMembership(rank, freq float64) float64 {
	threshold := 1 - freq
	const width = 0.05
	return 1 / (1 + math.Exp(-(rank-threshold)/width))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func OpeningRange(position int, rank float64) float64 {
	return rangeMembership(rank, openFreq(position))
}

func PreflopActionLikelihood(action ActionType, position int, rank float64) float64 {
	open := openFreq(position)

	switch action {
	case ActionFold:
		return clamp01(1 - rangeMembership(rank, open))

	case ActionCall:
		member := rangeMembership(rank, math.Min(open*1.6, 0.70))
		raisey := rangeMembership(rank, open*0.4)
		return clamp01(member*(1-0.55*raisey) + 0.02)

	case ActionCheck:
		member := rangeMembership(rank, 0.65)
		raisey := rangeMembership(rank, open*0.5)
		return clamp01(member*(1-0.5*raisey)*0.8 + 0.02)

	case ActionBet33, ActionBet50, ActionBet75, ActionBetPot:
		scale := map[ActionType]float64{
			ActionBet33: 1.15, ActionBet50: 1.0, ActionBet75: 0.9, ActionBetPot: 0.8,
		}[action]
		return clamp01(rangeMembership(rank, open*scale) + 0.02)

	case ActionAllIn:
		return clamp01(rangeMembership(rank, 0.06) + 0.02)
	}
	return 0.05
}
