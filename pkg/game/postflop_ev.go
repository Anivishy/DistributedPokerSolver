package game

import "math"

func callFreq(frac float64) float64 {
	return 1.0 / (1.0 + frac)
}

func callEquity(eq, frac float64) float64 {
	return math.Pow(clamp01(eq), 1.0+1.5*frac)
}

func BetEV(eq, frac float64) float64 {
	if frac <= 0 {
		return CheckEV(eq)
	}
	cf := callFreq(frac)
	foldFreq := 1.0 - cf
	whenCalled := callEquity(eq, frac)*(1.0+2.0*frac) - frac
	return foldFreq*1.0 + cf*whenCalled
}

func CheckEV(eq float64) float64 {
	return eq * 0.9
}

func CallEV(eq float64) float64 {
	return eq*1.3 - 0.4
}

func PostflopActionEVs(eq, stackToPot float64) []float64 {
	evs := make([]float64, NumActions)
	evs[ActionFold] = 0
	evs[ActionCheck] = CheckEV(eq)
	evs[ActionCall] = CallEV(eq)

	allInFrac := stackToPot
	if allInFrac < 1.0 {
		allInFrac = 1.0
	}
	fracs := map[ActionType]float64{
		ActionBet33: 0.33, ActionBet50: 0.5, ActionBet75: 0.75,
		ActionBetPot: 1.0, ActionAllIn: allInFrac,
	}
	for action, frac := range fracs {
		evs[action] = BetEV(eq, frac)
	}
	return evs
}
