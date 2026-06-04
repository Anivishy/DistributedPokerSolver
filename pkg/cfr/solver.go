package cfr

import (
	"fmt"
	"math"
	"sync"

	"poker-solver/pkg/deck"
	"poker-solver/pkg/game"
)

type StrategyEntry struct {
	mu          sync.Mutex
	regretSum   []float64
	strategySum []float64
	numActions  int
}

func (e *StrategyEntry) CurrentStrategy() []float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	strat := make([]float64, e.numActions)
	total := 0.0
	for _, r := range e.regretSum {
		if r > 0 {
			total += r
		}
	}
	if total <= 0 {
		for i := range strat {
			strat[i] = 1.0 / float64(e.numActions)
		}
		return strat
	}
	for i, r := range e.regretSum {
		if r > 0 {
			strat[i] = r / total
		}
	}
	return strat
}

func (e *StrategyEntry) AverageStrategy() []float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	avg := make([]float64, e.numActions)
	total := 0.0
	for _, s := range e.strategySum {
		total += s
	}
	if total <= 0 {
		for i := range avg {
			avg[i] = 1.0 / float64(e.numActions)
		}
		return avg
	}
	for i, s := range e.strategySum {
		avg[i] = s / total
	}
	return avg
}

func (e *StrategyEntry) AddRegret(regrets []float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, r := range regrets {
		if i < len(e.regretSum) {
			e.regretSum[i] += r
			if e.regretSum[i] < 0 {
				e.regretSum[i] = 0
			}
		}
	}
}

func (e *StrategyEntry) AccumulateStrategy(strategy []float64, weight float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, s := range strategy {
		if i < len(e.strategySum) {
			e.strategySum[i] += s * weight
		}
	}
}

type StrategyTable struct {
	mu         sync.RWMutex
	entries    map[string]*StrategyEntry
	numActions int
}

func NewStrategyTable(numActions int) *StrategyTable {
	return &StrategyTable{
		entries:    make(map[string]*StrategyEntry),
		numActions: numActions,
	}
}

func (t *StrategyTable) GetOrCreate(key string) *StrategyEntry {
	t.mu.RLock()
	e, ok := t.entries[key]
	t.mu.RUnlock()
	if ok {
		return e
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok = t.entries[key]; ok {
		return e
	}
	e = &StrategyEntry{
		numActions:  t.numActions,
		regretSum:   make([]float64, t.numActions),
		strategySum: make([]float64, t.numActions),
	}
	t.entries[key] = e
	return e
}

func (t *StrategyTable) Get(key string) (*StrategyEntry, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	e, ok := t.entries[key]
	return e, ok
}

type RegretUpdate struct {
	Key      string    `json:"key"`
	Regrets  []float64 `json:"regrets"`
	Strategy []float64 `json:"strategy"`
	Weight   float64   `json:"weight"`
}

func (t *StrategyTable) Merge(updates []RegretUpdate) {
	for _, u := range updates {
		e := t.GetOrCreate(u.Key)
		e.AddRegret(u.Regrets)
		e.AccumulateStrategy(u.Strategy, u.Weight)
	}
}

type Solver struct {
	table *StrategyTable
	abs   *game.Abstraction
}

func NewSolver(table *StrategyTable, abs *game.Abstraction) *Solver {
	return &Solver{table: table, abs: abs}
}

func (s *Solver) TraverseInfoSets(keys []string, strategies map[string][]float64, iteration int) []RegretUpdate {
	updates := make([]RegretUpdate, 0, len(keys))
	weight := math.Log(float64(iteration + 2))

	for _, key := range keys {
		strat, ok := strategies[key]
		if !ok || len(strat) != s.table.numActions {
			strat = uniformStrategy(s.table.numActions)
		}

		street, bucket, position := parseKeyFields(key)
		numBuckets := 20
		if street == 0 {
			numBuckets = 18
		}

		cfvs := make([]float64, s.table.numActions)
		nodeEV := 0.0
		for i := range cfvs {
			cfvs[i] = abstractActionValue(i, iteration, bucket, numBuckets, position, street)
			nodeEV += strat[i] * cfvs[i]
		}

		regrets := make([]float64, s.table.numActions)
		for i := range regrets {
			regrets[i] = cfvs[i] - nodeEV
		}

		updates = append(updates, RegretUpdate{
			Key:      key,
			Regrets:  regrets,
			Strategy: strat,
			Weight:   weight,
		})
	}
	return updates
}

func parseKeyFields(key string) (street, bucket, position int) {
	fmt.Sscanf(key, "s%d:b%d:p%d:", &street, &bucket, &position)
	return
}

func uniformStrategy(n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = 1.0 / float64(n)
	}
	return s
}

func abstractActionValue(actionIdx, iteration, bucket, numBuckets, position, street int) float64 {
	ep := float64(bucket) / float64(max(numBuckets-1, 1))
	posBonus := (float64(position) - 2.5) * 0.02
	noise := 0.05 / math.Sqrt(float64(iteration+2))

	if street == 0 {
		switch actionIdx {
		case 0:
			return 0.10 - ep*0.65 + noise
		case 1:
			return -0.08 + ep*0.10 + posBonus + noise
		case 2:
			return ep*0.28 - 0.09 + posBonus + noise
		case 3:
			return ep*0.42 - 0.19 + posBonus + noise
		case 4:
			return ep*0.44 - 0.195 + posBonus + noise
		case 5:
			return ep*0.46 - 0.20 + posBonus + noise
		case 6:
			return ep*0.46 - 0.21 + posBonus + noise
		case 7:
			return ep*0.60 - 0.38 + posBonus + noise
		}
		return 0
	}

	const stackToPot = 3.0
	switch actionIdx {
	case 0:
		return noise
	case 1:
		return game.CheckEV(ep) + noise
	case 2:
		return game.CallEV(ep) + posBonus + noise
	case 3:
		return game.BetEV(ep, 0.33) + posBonus + noise
	case 4:
		return game.BetEV(ep, 0.5) + posBonus + noise
	case 5:
		return game.BetEV(ep, 0.75) + posBonus + noise
	case 6:
		return game.BetEV(ep, 1.0) + posBonus + noise
	case 7:
		return game.BetEV(ep, stackToPot) + posBonus + noise
	}
	return 0
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *Solver) BestResponseValue(key string) float64 {
	e, ok := s.table.Get(key)
	if !ok {
		return 0
	}
	avg := e.AverageStrategy()
	cur := e.CurrentStrategy()
	diff := 0.0
	for i := range avg {
		d := avg[i] - cur[i]
		diff += d * d
	}
	return math.Sqrt(diff)
}

type ActionEV struct {
	Action game.ActionType
	EV     float64
}

func ComputeBestResponse(
	holdingIndices []int,
	opponentWeights []float64,
	street int,
	potSize, stackSize float64,
	board []deck.Card,
	abs *game.Abstraction,
	table *StrategyTable,
) []ActionEV {
	all := deck.All1326()
	results := make([]ActionEV, 0, len(holdingIndices)*game.NumActions)

	for _, hIdx := range holdingIndices {
		if hIdx < 0 || hIdx >= len(all) {
			continue
		}
		hero := all[hIdx]
		evs := actionEVsVsRange(hero, opponentWeights, all, board, potSize, stackSize)
		for aIdx, ev := range evs {
			results = append(results, ActionEV{Action: game.ActionType(aIdx), EV: ev})
		}
	}
	return results
}

func actionEVsVsRange(
	hero deck.Combo,
	oppWeights []float64,
	all []deck.Combo,
	board []deck.Card,
	pot, stack float64,
) []float64 {
	evs := make([]float64, game.NumActions)

	eq := equityVsRange(hero, oppWeights, all, board)

	evs[game.ActionFold] = 0
	evs[game.ActionCheck] = eq*pot - (1-eq)*0
	evs[game.ActionCall] = eq*(pot+stack*0.3) - (1-eq)*stack*0.3
	betSizes := []float64{0.33, 0.5, 0.75, 1.0, stack / pot}
	for i, frac := range betSizes {
		bet := pot * frac
		if bet > stack {
			bet = stack
		}
		aIdx := int(game.ActionBet33) + i
		if aIdx < game.NumActions {
			evs[aIdx] = eq*(pot+bet) - (1-eq)*bet
		}
	}
	return evs
}

func equityVsRange(hero deck.Combo, oppWeights []float64, all []deck.Combo, board []deck.Card) float64 {
	heroCards := []deck.Card{hero.C1, hero.C2}
	totalWeight, winWeight := 0.0, 0.0

	for oppIdx, w := range oppWeights {
		if w <= 0 || oppIdx >= len(all) {
			continue
		}
		opp := all[oppIdx]
		if opp.BlockedBy(heroCards) || opp.BlockedBy(board) {
			continue
		}

		heroWinProb := game.HeadsUpEquity(heroCards, []deck.Card{opp.C1, opp.C2}, board, 200)

		winWeight += heroWinProb * w
		totalWeight += w
	}

	if totalWeight <= 0 {
		return 0.5
	}
	return winWeight / totalWeight
}
