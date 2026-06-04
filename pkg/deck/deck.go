package deck

import (
	"fmt"
	"math/rand"
	"strings"
)

type Card struct {
	Rank int
	Suit int
}

type Combo struct {
	C1, C2 Card
}

type Deck struct {
	cards []Card
}

func NewDeck() *Deck {
	d := &Deck{}
	for rank := 2; rank <= 14; rank++ {
		for suit := 0; suit < 4; suit++ {
			d.cards = append(d.cards, Card{rank, suit})
		}
	}
	return d
}

func (c Card) Index() int {
	return (c.Rank-2)*4 + c.Suit
}

func CardFromIndex(idx int) Card {
	return Card{Rank: idx/4 + 2, Suit: idx % 4}
}

func All52() []Card {
	cards := make([]Card, 52)
	for i := 0; i < 52; i++ {
		cards[i] = CardFromIndex(i)
	}
	return cards
}

func All1326() []Combo {
	all := All52()
	combos := make([]Combo, 0, 1326)
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			combos = append(combos, Combo{C1: all[i], C2: all[j]})
		}
	}
	return combos
}

func (co Combo) Index() int {
	i1, i2 := co.C1.Index(), co.C2.Index()
	if i1 > i2 {
		i1, i2 = i2, i1
	}
	return i1*52 - i1*(i1+1)/2 + i2 - i1 - 1
}

func (co Combo) BlockedBy(blocked []Card) bool {
	for _, b := range blocked {
		if co.C1 == b || co.C2 == b {
			return true
		}
	}
	return false
}

func (d *Deck) Shuffle(r *rand.Rand) {
	r.Shuffle(len(d.cards), func(i, j int) {
		d.cards[i], d.cards[j] = d.cards[j], d.cards[i]
	})
}

func (d *Deck) Deal(n int) []Card {
	if n > len(d.cards) {
		n = len(d.cards)
	}
	dealt := make([]Card, n)
	copy(dealt, d.cards[:n])
	d.cards = d.cards[n:]
	return dealt
}

func (d *Deck) Remove(cards []Card) {
	keep := make([]Card, 0, len(d.cards))
	for _, c := range d.cards {
		blocked := false
		for _, r := range cards {
			if c == r {
				blocked = true
				break
			}
		}
		if !blocked {
			keep = append(keep, c)
		}
	}
	d.cards = keep
}

var rankNames = map[int]string{
	2: "2", 3: "3", 4: "4", 5: "5", 6: "6", 7: "7",
	8: "8", 9: "9", 10: "T", 11: "J", 12: "Q", 13: "K", 14: "A",
}
var suitNames = []string{"c", "d", "h", "s"}

func (c Card) String() string {
	return rankNames[c.Rank] + suitNames[c.Suit]
}

func Parse(s string) (Card, error) {
	if len(s) != 2 {
		return Card{}, fmt.Errorf("invalid card %q: must be 2 characters", s)
	}
	rankMap := map[byte]int{
		'2': 2, '3': 3, '4': 4, '5': 5, '6': 6, '7': 7, '8': 8,
		'9': 9, 'T': 10, 't': 10, 'J': 11, 'j': 11,
		'Q': 12, 'q': 12, 'K': 13, 'k': 13, 'A': 14, 'a': 14,
	}
	suitMap := map[byte]int{'c': 0, 'C': 0, 'd': 1, 'D': 1, 'h': 2, 'H': 2, 's': 3, 'S': 3}

	rankByte := strings.ToUpper(string(s[0]))[0]
	r, ok := rankMap[rankByte]
	if !ok {
		return Card{}, fmt.Errorf("invalid rank %q in card %q", s[0], s)
	}
	su, ok := suitMap[s[1]]
	if !ok {
		return Card{}, fmt.Errorf("invalid suit %q in card %q", s[1], s)
	}
	return Card{Rank: r, Suit: su}, nil
}
