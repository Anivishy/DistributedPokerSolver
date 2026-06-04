package cfr

import (
	"encoding/gob"
	"os"
	"path/filepath"
)

type entrySnapshot struct {
	RegretSum   []float64
	StrategySum []float64
}

type tableSnapshot struct {
	NumActions int
	Entries    map[string]entrySnapshot
}

func (t *StrategyTable) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	t.mu.RLock()
	snap := tableSnapshot{
		NumActions: t.numActions,
		Entries:    make(map[string]entrySnapshot, len(t.entries)),
	}
	for k, e := range t.entries {
		e.mu.Lock()
		snap.Entries[k] = entrySnapshot{
			RegretSum:   append([]float64{}, e.regretSum...),
			StrategySum: append([]float64{}, e.strategySum...),
		}
		e.mu.Unlock()
	}
	t.mu.RUnlock()

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return gob.NewEncoder(f).Encode(snap)
}

func (t *StrategyTable) LoadInto(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var snap tableSnapshot
	if err := gob.NewDecoder(f).Decode(&snap); err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	for k, es := range snap.Entries {
		t.entries[k] = &StrategyEntry{
			numActions:  t.numActions,
			regretSum:   es.RegretSum,
			strategySum: es.StrategySum,
		}
	}
	return nil
}
