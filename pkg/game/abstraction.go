package game

import (
	"encoding/gob"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"poker-solver/pkg/deck"
)

type ActionType int

const (
	ActionFold   ActionType = 0
	ActionCheck  ActionType = 1
	ActionCall   ActionType = 2
	ActionBet33  ActionType = 3
	ActionBet50  ActionType = 4
	ActionBet75  ActionType = 5
	ActionBetPot ActionType = 6
	ActionAllIn  ActionType = 7
)

var ActionNames = []string{
	"Fold", "Check", "Call", "Bet 33%", "Bet 50%", "Bet 75%", "Bet Pot", "All-in",
}

const NumActions = 8

type Abstraction struct {
	numPreflopBuckets  int
	numPostflopBuckets int
	preflopEquities    []float64
}

func NewAbstraction(numPreflop, numPostflop int) *Abstraction {
	return &Abstraction{
		numPreflopBuckets:  numPreflop,
		numPostflopBuckets: numPostflop,
	}
}

func (a *Abstraction) BuildPreflopTable() {
	all := deck.All1326()
	equities := make([]float64, len(all))
	for i, combo := range all {
		equities[i] = Equity([]deck.Card{combo.C1, combo.C2}, nil, 15)
	}
	a.preflopEquities = equities
}

func (a *Abstraction) SavePreflopTable(path string) error {
	if len(a.preflopEquities) == 0 {
		return fmt.Errorf("preflop table not built")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return gob.NewEncoder(f).Encode(a.preflopEquities)
}

func (a *Abstraction) LoadPreflopTable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return gob.NewDecoder(f).Decode(&a.preflopEquities)
}

func (a *Abstraction) SetPreflopEquities(equities []float64) {
	a.preflopEquities = equities
}

func (a *Abstraction) HasPreflopTable() bool {
	return len(a.preflopEquities) > 0
}

func (a *Abstraction) PreflopEquities() []float64 {
	return a.preflopEquities
}

func (a *Abstraction) PreflopBucket(combo deck.Combo, position int) int {
	rank := PreflopHandRank(combo)
	posMod := positionBucketMod(position)
	raw := int(rank*float64(a.numPreflopBuckets)) + posMod
	return clampBucket(raw, a.numPreflopBuckets)
}

func (a *Abstraction) PostflopBucket(hole, board []deck.Card, inPosition bool) int {
	eq := Equity(hole, board, 15)
	ehs2 := eq * eq
	tex := ClassifyBoard(board)

	texMod := 0.0
	if tex.Wetness > 0.6 {
		texMod = 0.05
	} else if tex.Wetness < 0.2 {
		texMod = -0.05
	}
	posMod := 0.0
	if inPosition {
		posMod = 0.03
	}

	score := 0.7*eq + 0.2*ehs2 + texMod + posMod
	raw := int(score * float64(a.numPostflopBuckets))
	return clampBucket(raw, a.numPostflopBuckets)
}

func positionBucketMod(position int) int {
	switch position {
	case 3, 4, 2:
		return 1
	case 0:
		return -1
	}
	return 0
}

func clampBucket(v, max int) int {
	if v < 0 {
		return 0
	}
	if v >= max {
		return max - 1
	}
	return v
}

type InfoSetKey struct {
	Street         int
	Bucket         int
	Position       int
	ActionHistory  string
	PotSize        int
	StackDepth     int
	BoardTexBucket int
}

func (k InfoSetKey) String() string {
	return fmt.Sprintf("s%d:b%d:p%d:h%s:pot%d:stk%d:tex%d",
		k.Street, k.Bucket, k.Position, k.ActionHistory,
		k.PotSize, k.StackDepth, k.BoardTexBucket)
}

func NearestStackDepth(stack int) int {
	depths := []int{50, 100}
	best := depths[0]
	for _, d := range depths {
		if intAbs(stack-d) < intAbs(stack-best) {
			best = d
		}
	}
	return best
}

func NearestPotSize(pot int) int {
	candidates := []int{10, 25, 75, 200}
	best := candidates[0]
	for _, c := range candidates {
		if intAbs(pot-c) < intAbs(pot-best) {
			best = c
		}
	}
	return best
}

func NearestTexBucket(tex int) int {
	if tex <= 1 {
		return 0
	}
	if tex <= 4 {
		return 3
	}
	return 6
}

func intAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func TexBucket(tex BoardTexture) int {
	b := int(math.Min(tex.Wetness*8, 7))
	if b < 0 {
		b = 0
	}
	return b
}

func GenerateInfoSetKeys(numPreflop, numPostflop int) []string {
	streets := []int{0, 1, 2, 3}
	positions := []int{0, 1, 2, 3, 4, 5}
	potSizes := []int{10, 25, 75, 200}
	stackDepths := []int{50, 100}

	var keys []string
	for _, street := range streets {
		numBuckets := numPostflop
		texBuckets := []int{0, 3, 6}
		if street == 0 {
			numBuckets = numPreflop
			texBuckets = []int{0}
		}
		for bucket := 0; bucket < numBuckets; bucket++ {
			for _, pos := range positions {
				for _, pot := range potSizes {
					for _, stk := range stackDepths {
						for _, tex := range texBuckets {
							k := InfoSetKey{
								Street:         street,
								Bucket:         bucket,
								Position:       pos,
								ActionHistory:  "",
								PotSize:        pot,
								StackDepth:     stk,
								BoardTexBucket: tex,
							}
							keys = append(keys, k.String())
						}
					}
				}
			}
		}
	}
	return keys
}
