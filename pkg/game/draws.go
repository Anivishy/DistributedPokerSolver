package game

import "poker-solver/pkg/deck"

func DrawStrength(hole, board []deck.Card) float64 {
	if len(board) < 3 || len(board) > 4 {
		return 0
	}
	cards := make([]deck.Card, 0, len(hole)+len(board))
	cards = append(cards, hole...)
	cards = append(cards, board...)

	flush := flushDrawStrength(cards, hole)
	straight := straightDrawStrength(cards, hole)
	if flush > straight {
		return flush
	}
	return straight
}

func flushDrawStrength(cards, hole []deck.Card) float64 {
	var suitCount [4]int
	for _, c := range cards {
		suitCount[c.Suit]++
	}
	for suit, n := range suitCount {
		if n != 4 {
			continue
		}
		for _, h := range hole {
			if h.Suit == suit {
				return 0.9
			}
		}
	}
	return 0
}

func straightDrawStrength(cards, hole []deck.Card) float64 {
	var present [15]bool
	var fromHole [15]bool
	for _, c := range cards {
		present[c.Rank] = true
	}
	for _, h := range hole {
		fromHole[h.Rank] = true
	}
	if present[14] {
		present[1] = true
	}
	if fromHole[14] {
		fromHole[1] = true
	}

	best := 0.0
	for low := 1; low <= 10; low++ {
		count, holeInWindow := 0, false
		for r := low; r < low+5; r++ {
			if present[r] {
				count++
				if fromHole[r] {
					holeInWindow = true
				}
			}
		}
		if !holeInWindow || count != 4 {
			continue
		}
		edgeMissing := !present[low] || !present[low+4]
		if edgeMissing {
			if 0.70 > best {
				best = 0.70
			}
		} else if 0.35 > best {
			best = 0.35
		}
	}
	return best
}
