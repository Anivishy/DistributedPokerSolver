package game

import (
	"math/rand"

	"poker-solver/pkg/deck"
)

func showdownResult(hero, opp, fullBoard []deck.Card) float64 {
	hv := Evaluate(append(append([]deck.Card{}, hero...), fullBoard...))
	ov := Evaluate(append(append([]deck.Card{}, opp...), fullBoard...))
	switch {
	case hv > ov:
		return 1
	case hv == ov:
		return 0.5
	default:
		return 0
	}
}

func HeadsUpEquity(hero, opp, board []deck.Card, samples int) float64 {
	need := 5 - len(board)
	if need <= 0 {
		return showdownResult(hero, opp, board)
	}

	known := make([]deck.Card, 0, len(hero)+len(opp)+len(board))
	known = append(known, hero...)
	known = append(known, opp...)
	known = append(known, board...)

	remaining := make([]deck.Card, 0, 52)
	for _, c := range deck.All52() {
		if !cardIn(c, known) {
			remaining = append(remaining, c)
		}
	}
	if need > len(remaining) {
		return 0.5
	}

	full := make([]deck.Card, len(board)+need)
	copy(full, board)
	runout := full[len(board):]

	wins, total := 0.0, 0.0

	if need <= 2 {
		var rec func(start, depth int)
		rec = func(start, depth int) {
			if depth == need {
				wins += showdownResult(hero, opp, full)
				total++
				return
			}
			for i := start; i <= len(remaining)-(need-depth); i++ {
				runout[depth] = remaining[i]
				rec(i+1, depth+1)
			}
		}
		rec(0, 0)
	} else {
		r := rand.New(rand.NewSource(seedFromCards(known)))
		pool := make([]deck.Card, len(remaining))
		for s := 0; s < samples; s++ {
			copy(pool, remaining)
			for k := 0; k < need; k++ {
				j := k + r.Intn(len(pool)-k)
				pool[k], pool[j] = pool[j], pool[k]
				runout[k] = pool[k]
			}
			wins += showdownResult(hero, opp, full)
			total++
		}
	}

	if total == 0 {
		return 0.5
	}
	return wins / total
}

func seedFromCards(cards []deck.Card) int64 {
	var s int64 = 1469598103934665603
	for _, c := range cards {
		s = (s ^ int64(c.Index()+1)) * 1099511628211
	}
	return s
}
