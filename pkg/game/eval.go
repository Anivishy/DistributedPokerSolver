package game

import (
	"poker-solver/pkg/deck"
)

type HandValue uint64

const (
	catHighCard      = 1
	catOnePair       = 2
	catTwoPair       = 3
	catTrips         = 4
	catStraight      = 5
	catFlush         = 6
	catFullHouse     = 7
	catQuads         = 8
	catStraightFlush = 9
)

func Evaluate(cards []deck.Card) HandValue {
	if len(cards) == 5 {
		return eval5(cards)
	}
	best := HandValue(0)
	n := len(cards)
	var combo [5]deck.Card
	var try func(start, depth int)
	try = func(start, depth int) {
		if depth == 5 {
			if v := eval5(combo[:]); v > best {
				best = v
			}
			return
		}
		for i := start; i <= n-5+depth; i++ {
			combo[depth] = cards[i]
			try(i+1, depth+1)
		}
	}
	try(0, 0)
	return best
}

func encodeHV(cat, tb1, tb2, tb3, tb4, tb5 int) HandValue {
	return HandValue(uint64(cat)<<56 | uint64(tb1)<<48 | uint64(tb2)<<40 |
		uint64(tb3)<<32 | uint64(tb4)<<24 | uint64(tb5)<<16)
}

func eval5(cards []deck.Card) HandValue {
	var rankCounts [15]int
	var suitCounts [4]int
	for _, c := range cards {
		rankCounts[c.Rank]++
		suitCounts[c.Suit]++
	}

	isFlush := false
	for _, sc := range suitCounts {
		if sc == 5 {
			isFlush = true
			break
		}
	}

	ranks := make([]int, 0, 5)
	for r := 14; r >= 2; r-- {
		if rankCounts[r] > 0 {
			ranks = append(ranks, r)
		}
	}

	isStraight := false
	straightHigh := 0
	if len(ranks) == 5 {
		if ranks[0]-ranks[4] == 4 {
			isStraight = true
			straightHigh = ranks[0]
		} else if ranks[0] == 14 && ranks[1] == 5 && ranks[4] == 2 {
			isStraight = true
			straightHigh = 5
		}
	}

	var quads, trips, pairs, singles []int
	for r := 14; r >= 2; r-- {
		switch rankCounts[r] {
		case 4:
			quads = append(quads, r)
		case 3:
			trips = append(trips, r)
		case 2:
			pairs = append(pairs, r)
		case 1:
			singles = append(singles, r)
		}
	}

	if isFlush && isStraight {
		return encodeHV(catStraightFlush, straightHigh, 0, 0, 0, 0)
	}
	if len(quads) > 0 {
		kicker := 0
		if len(singles) > 0 {
			kicker = singles[0]
		} else if len(pairs) > 0 {
			kicker = pairs[0]
		} else if len(trips) > 0 {
			kicker = trips[0]
		}
		return encodeHV(catQuads, quads[0], kicker, 0, 0, 0)
	}
	if len(trips) > 0 && len(pairs) > 0 {
		return encodeHV(catFullHouse, trips[0], pairs[0], 0, 0, 0)
	}
	if isFlush {
		k := [5]int{}
		for i, r := range ranks {
			if i < 5 {
				k[i] = r
			}
		}
		return encodeHV(catFlush, k[0], k[1], k[2], k[3], k[4])
	}
	if isStraight {
		return encodeHV(catStraight, straightHigh, 0, 0, 0, 0)
	}
	if len(trips) > 0 {
		k1, k2 := 0, 0
		if len(singles) >= 1 {
			k1 = singles[0]
		}
		if len(singles) >= 2 {
			k2 = singles[1]
		}
		return encodeHV(catTrips, trips[0], k1, k2, 0, 0)
	}
	if len(pairs) >= 2 {
		k := 0
		if len(singles) > 0 {
			k = singles[0]
		}
		return encodeHV(catTwoPair, pairs[0], pairs[1], k, 0, 0)
	}
	if len(pairs) == 1 {
		k1, k2, k3 := 0, 0, 0
		if len(singles) >= 1 {
			k1 = singles[0]
		}
		if len(singles) >= 2 {
			k2 = singles[1]
		}
		if len(singles) >= 3 {
			k3 = singles[2]
		}
		return encodeHV(catOnePair, pairs[0], k1, k2, k3, 0)
	}
	k := [5]int{}
	for i, r := range singles {
		if i < 5 {
			k[i] = r
		}
	}
	return encodeHV(catHighCard, k[0], k[1], k[2], k[3], k[4])
}

func Equity(hole, board []deck.Card, numSamples int) float64 {
	blocked := make([]deck.Card, 0, len(hole)+len(board))
	blocked = append(blocked, hole...)
	blocked = append(blocked, board...)

	remaining := make([]deck.Card, 0, 52)
	for _, c := range deck.All52() {
		if !cardIn(c, blocked) {
			remaining = append(remaining, c)
		}
	}

	need := 5 - len(board)
	wins, total := 0.0, 0.0

	for i := 0; i < len(remaining); i++ {
		for j := i + 1; j < len(remaining); j++ {
			opp := []deck.Card{remaining[i], remaining[j]}
			rem2 := make([]deck.Card, 0, len(remaining)-2)
			for k, c := range remaining {
				if k != i && k != j {
					rem2 = append(rem2, c)
				}
			}
			runouts := sampleRunouts(rem2, need, numSamples)
			for _, runout := range runouts {
				heroBoard := append(append([]deck.Card{}, board...), runout...)
				oppBoard := heroBoard
				hv := Evaluate(append(append([]deck.Card{}, hole...), heroBoard...))
				ov := Evaluate(append(append([]deck.Card{}, opp...), oppBoard...))
				if hv > ov {
					wins++
				} else if hv == ov {
					wins += 0.5
				}
				total++
			}
		}
	}

	if total == 0 {
		return 0.5
	}
	return wins / total
}

func sampleRunouts(remaining []deck.Card, n, samples int) [][]deck.Card {
	if n <= 0 {
		return [][]deck.Card{{}}
	}
	if n > len(remaining) {
		n = len(remaining)
	}
	if samples > len(remaining) {
		samples = len(remaining)
	}

	result := make([][]deck.Card, 0, samples)
	for s := 0; s < samples; s++ {
		runout := make([]deck.Card, n)
		for k := 0; k < n; k++ {
			runout[k] = remaining[(s+k)%len(remaining)]
		}
		result = append(result, runout)
	}
	return result
}

func cardIn(c deck.Card, cards []deck.Card) bool {
	for _, x := range cards {
		if c == x {
			return true
		}
	}
	return false
}

type BoardTexture struct {
	Paired    bool
	Monotone  bool
	TwoTone   bool
	Connected bool
	HighCard  int
	Wetness   float64
}

func ClassifyBoard(board []deck.Card) BoardTexture {
	if len(board) == 0 {
		return BoardTexture{}
	}
	var rankCounts [15]int
	var suitCounts [4]int
	for _, c := range board {
		rankCounts[c.Rank]++
		suitCounts[c.Suit]++
	}

	paired := false
	for _, cnt := range rankCounts {
		if cnt >= 2 {
			paired = true
			break
		}
	}

	maxSuit := 0
	for _, cnt := range suitCounts {
		if cnt > maxSuit {
			maxSuit = cnt
		}
	}
	monotone := maxSuit == len(board)
	twoTone := !monotone && maxSuit >= 2

	ranks := make([]int, 0, len(board))
	for r := 14; r >= 2; r-- {
		if rankCounts[r] > 0 {
			ranks = append(ranks, r)
		}
	}
	connected := len(ranks) >= 2 && (ranks[0]-ranks[len(ranks)-1]) <= 4

	highCard := 0
	if len(ranks) > 0 {
		highCard = ranks[0]
	}

	wetness := 0.0
	if monotone || twoTone {
		wetness += 0.4
	}
	if connected {
		wetness += 0.4
	}
	if highCard > 0 && highCard < 10 {
		wetness += 0.2
	}

	return BoardTexture{
		Paired:    paired,
		Monotone:  monotone,
		TwoTone:   twoTone,
		Connected: connected,
		HighCard:  highCard,
		Wetness:   wetness,
	}
}
