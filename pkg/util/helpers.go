package util

import (
	"math"

	"poker-solver/pkg/deck"
	"poker-solver/pkg/game"
)

func ParseCards(strs []string) []deck.Card {
	var cards []deck.Card
	for _, s := range strs {
		c, err := deck.Parse(s)
		if err == nil {
			cards = append(cards, c)
		}
	}
	return cards
}

func ParseActionType(s string) game.ActionType {
	m := map[string]game.ActionType{
		"fold":   game.ActionFold,
		"check":  game.ActionCheck,
		"call":   game.ActionCall,
		"bet33":  game.ActionBet33,
		"bet50":  game.ActionBet50,
		"bet75":  game.ActionBet75,
		"betpot": game.ActionBetPot,
		"allin":  game.ActionAllIn,
	}
	if a, ok := m[s]; ok {
		return a
	}
	return game.ActionCheck
}

func ActionLikelihood(action game.ActionType, equity float64) float64 {
	return ActionLikelihoodWithDraw(action, equity, 0)
}

func ActionLikelihoodWithDraw(action game.ActionType, equity, draw float64) float64 {
	base := actionLikelihoodMade(action, equity)
	if draw <= 0 {
		return base
	}
	switch action {
	case game.ActionFold:
		return base * (1 - 0.7*draw)
	case game.ActionCall:
		return math.Min(1.0, base+0.3*draw)
	case game.ActionBet33, game.ActionBet50:
		return math.Min(1.0, base+0.5*draw)
	case game.ActionBet75, game.ActionBetPot:
		return math.Min(1.0, base+0.35*draw)
	case game.ActionAllIn:
		return math.Min(1.0, base+0.15*draw)
	default:
		return base
	}
}

func actionLikelihoodMade(action game.ActionType, equity float64) float64 {
	clamp := func(v, lo, hi float64) float64 {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	e := equity

	switch action {
	case game.ActionFold:
		return clamp(1.2-e*2.8, 0.01, 1.0)

	case game.ActionCheck:
		if e <= 0.60 {
			return clamp(0.90-math.Pow((e-0.42)*2.5, 2)*0.75, 0.05, 0.90)
		}
		return clamp(0.70-(e-0.60)*2.5, 0.05, 0.70)

	case game.ActionCall:
		if e < 0.42 {
			return 0.04
		}
		if e > 0.82 {
			return 0.30
		}
		return clamp(0.25+(e-0.42)*1.9, 0.04, 0.80)

	case game.ActionBet33, game.ActionBet50:
		if e >= 0.62 {
			return clamp(0.35+(e-0.62)*1.8, 0.10, 0.95)
		}
		if e < 0.25 {
			return clamp(0.20-e*0.5, 0.05, 0.22)
		}
		return 0.08

	case game.ActionBet75, game.ActionBetPot:
		if e >= 0.72 {
			return clamp(0.40+(e-0.72)*2.5, 0.10, 0.98)
		}
		if e < 0.18 {
			return clamp(0.18-e*0.6, 0.03, 0.20)
		}
		return 0.04

	case game.ActionAllIn:
		if e >= 0.80 {
			return clamp(0.45+(e-0.80)*3.5, 0.10, 0.99)
		}
		if e < 0.12 {
			return 0.10
		}
		return 0.02
	}
	return 0.10
}
