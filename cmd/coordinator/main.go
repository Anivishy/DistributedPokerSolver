package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"poker-solver/pkg/cfr"
	"poker-solver/pkg/deck"
	"poker-solver/pkg/game"
	pkg_kafka "poker-solver/pkg/kafka"
	"poker-solver/pkg/util"

	kafkago "github.com/segmentio/kafka-go"
)

const dataDir = "/data"

type Config struct {
	Port          string
	WorkerAddrs   []string
	NumIterations int
	KafkaAddr     string
}

func configFromEnv() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addrs := strings.Split(os.Getenv("WORKER_ADDRS"), ",")
	if len(addrs) == 1 && addrs[0] == "" {
		addrs = []string{"worker1:9001", "worker2:9001", "worker3:9001", "worker4:9001"}
	}
	iters, _ := strconv.Atoi(os.Getenv("NUM_ITERATIONS"))
	if iters <= 0 {
		iters = 2000
	}
	kafkaAddr := os.Getenv("KAFKA_ADDR")
	if kafkaAddr == "" {
		kafkaAddr = "kafka:9092"
	}
	return Config{Port: port, WorkerAddrs: addrs, NumIterations: iters, KafkaAddr: kafkaAddr}
}

type Coordinator struct {
	cfg         Config
	table       *cfr.StrategyTable
	abstraction *game.Abstraction
	solved      int32
	total       int32

	kafkaWriters []*kafkago.Writer
	kafkaReader  *kafkago.Reader
	pending      sync.Map
}

type pendingRange struct {
	ch       chan pkg_kafka.RangeResultMsg
	expected int
}

func NewCoordinator(cfg Config) *Coordinator {
	abs := game.NewAbstraction(18, 20)
	table := cfr.NewStrategyTable(game.NumActions)
	return &Coordinator{cfg: cfg, table: table, abstraction: abs}
}

func (c *Coordinator) initKafka() {
	numWorkers := len(c.cfg.WorkerAddrs)
	c.kafkaWriters = make([]*kafkago.Writer, numWorkers)
	for i := range numWorkers {
		c.kafkaWriters[i] = pkg_kafka.NewWriter(c.cfg.KafkaAddr, pkg_kafka.RangeRequestTopic(i))
	}
	c.kafkaReader = pkg_kafka.NewReader(c.cfg.KafkaAddr, pkg_kafka.TopicRangeResults)
	log.Printf("Kafka initialised: broker=%s, %d request topics + results topic", c.cfg.KafkaAddr, numWorkers)
}

func (c *Coordinator) runResultConsumer(ctx context.Context) {
	for {
		msg, err := c.kafkaReader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("kafka result consumer read error: %v", err)
			continue
		}
		var result pkg_kafka.RangeResultMsg
		if err := pkg_kafka.Decode(msg, &result); err != nil {
			log.Printf("kafka result consumer decode error: %v", err)
			continue
		}
		val, ok := c.pending.Load(result.RequestID)
		if !ok {
			continue
		}
		pr := val.(*pendingRange)
		select {
		case pr.ch <- result:
		default:
		}
	}
}

func (c *Coordinator) loadOrBuild() {
	preflopPath := filepath.Join(dataDir, "preflop_equities.gob")
	strategyPath := filepath.Join(dataDir, "strategy_table.gob")

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Printf("Warning: cannot create data dir: %v", err)
	}

	if err := c.abstraction.LoadPreflopTable(preflopPath); err == nil {
		log.Println("Preflop equity table loaded from disk")
	} else {
		log.Printf("Building preflop equity table across %d workers…", len(c.cfg.WorkerAddrs))
		c.distributedBuildPreflopTable()
		if err := c.abstraction.SavePreflopTable(preflopPath); err != nil {
			log.Printf("Warning: failed to save preflop table: %v", err)
		} else {
			log.Println("Preflop equity table saved to disk")
		}
	}

	if err := c.table.LoadInto(strategyPath); err == nil {
		atomic.StoreInt32(&c.total, 1)
		atomic.StoreInt32(&c.solved, 1)
		log.Println("Strategy table loaded from disk — solver ready immediately")
		return
	}

	log.Printf("Running CFR+ pre-solve (%d iterations across %d workers)…", c.cfg.NumIterations, len(c.cfg.WorkerAddrs))
	c.RunPresolve()

	if err := c.table.Save(strategyPath); err != nil {
		log.Printf("Warning: failed to save strategy table: %v", err)
	} else {
		log.Println("Strategy table saved to disk — future starts will skip the solve")
	}
}

type PreflopShardRequest struct {
	ComboStart int `json:"combo_start"`
	ComboEnd   int `json:"combo_end"`
	NumSamples int `json:"num_samples"`
}

type PreflopShardResponse struct {
	WorkerID int       `json:"worker_id"`
	Equities []float64 `json:"equities"`
}

func (c *Coordinator) distributedBuildPreflopTable() {
	const total = 1326
	numWorkers := len(c.cfg.WorkerAddrs)
	shardSize := (total + numWorkers - 1) / numWorkers

	equities := make([]float64, total)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for workerIdx, addr := range c.cfg.WorkerAddrs {
		workerIdx, addr := workerIdx, addr
		start := workerIdx * shardSize
		end := start + shardSize
		if end > total {
			end = total
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := PreflopShardRequest{ComboStart: start, ComboEnd: end, NumSamples: 15}
			var resp PreflopShardResponse
			if err := postJSON("http://"+addr+"/preflop/shard", req, &resp); err != nil {
				log.Printf("preflop shard worker%d error: %v", workerIdx+1, err)
				return
			}
			mu.Lock()
			copy(equities[start:end], resp.Equities)
			mu.Unlock()
			log.Printf("Preflop shard worker%d done (%d combos)", workerIdx+1, end-start)
		}()
	}
	wg.Wait()
	c.abstraction.SetPreflopEquities(equities)
	log.Println("Distributed preflop equity table complete")
}

type CFRShardRequest struct {
	Keys       []string             `json:"keys"`
	Strategies map[string][]float64 `json:"strategies"`
	Iteration  int                  `json:"iteration"`
}

type CFRShardResponse struct {
	WorkerID int                `json:"worker_id"`
	Updates  []cfr.RegretUpdate `json:"updates"`
}

func (c *Coordinator) RunPresolve() {
	allKeys := game.GenerateInfoSetKeys(18, 20)
	atomic.StoreInt32(&c.total, int32(c.cfg.NumIterations))

	for iter := 0; iter < c.cfg.NumIterations; iter++ {
		shards := partitionStrings(allKeys, len(c.cfg.WorkerAddrs))

		type result struct {
			updates []cfr.RegretUpdate
			err     error
		}
		results := make([]result, len(c.cfg.WorkerAddrs))
		var wg sync.WaitGroup

		for workerIdx, addr := range c.cfg.WorkerAddrs {
			workerIdx, addr := workerIdx, addr
			wg.Add(1)
			go func() {
				defer wg.Done()
				shardKeys := shards[workerIdx]
				strategies := make(map[string][]float64, len(shardKeys))
				for _, key := range shardKeys {
					if e, ok := c.table.Get(key); ok {
						strategies[key] = e.CurrentStrategy()
					}
				}
				req := CFRShardRequest{Keys: shardKeys, Strategies: strategies, Iteration: iter}
				var resp CFRShardResponse
				if err := postJSON("http://"+addr+"/cfr/shard", req, &resp); err != nil {
					results[workerIdx] = result{err: err}
					return
				}
				results[workerIdx] = result{updates: resp.Updates}
			}()
		}
		wg.Wait()

		for _, r := range results {
			if r.err != nil {
				log.Printf("cfr shard error (iter %d): %v", iter, r.err)
				continue
			}
			c.table.Merge(r.updates)
		}
		atomic.AddInt32(&c.solved, 1)
	}
	log.Printf("Pre-solve complete: %d iterations, %d info sets", c.cfg.NumIterations, len(allKeys))
}

type GTORequest struct {
	HoleCards  []string `json:"hole_cards"`
	BoardCards []string `json:"board_cards"`
	Position   int      `json:"position"`
	PotSize    float64  `json:"pot_size"`
	StackSize  float64  `json:"stack_size"`
	Street     int      `json:"street"`
}

type ActionStrategy struct {
	Action    string  `json:"action"`
	Frequency float64 `json:"frequency"`
	EV        float64 `json:"ev"`
}

type GTOResponse struct {
	Strategy []ActionStrategy `json:"strategy"`
	Bucket   int              `json:"bucket"`
}

func (c *Coordinator) handleGTO(w http.ResponseWriter, r *http.Request) {
	var req GTORequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	hole := util.ParseCards(req.HoleCards)
	board := util.ParseCards(req.BoardCards)

	bucket := 0
	if req.Street == 0 && len(hole) == 2 {
		bucket = c.abstraction.PreflopBucket(deck.Combo{C1: hole[0], C2: hole[1]}, req.Position)
	} else if req.Street > 0 && len(hole) == 2 {
		bucket = c.abstraction.PostflopBucket(hole, board, req.Position >= 2)
	}

	pot := game.NearestPotSize(int(req.PotSize))
	stk := game.NearestStackDepth(int(req.StackSize))
	texBucket := 0
	if len(board) > 0 {
		texBucket = game.NearestTexBucket(game.TexBucket(game.ClassifyBoard(board)))
	}
	key := game.InfoSetKey{
		Street: req.Street, Bucket: bucket, Position: req.Position,
		PotSize: pot, StackDepth: stk, BoardTexBucket: texBucket,
	}

	log.Printf("GTO lookup: cards=%v pos=%d street=%d bucket=%d key=%s", req.HoleCards, req.Position, req.Street, bucket, key.String())
	var strategy []float64
	if e, ok := c.table.Get(key.String()); ok {
		strategy = e.AverageStrategy()
	} else {
		strategy = make([]float64, game.NumActions)
		for i := range strategy {
			strategy[i] = 1.0 / float64(game.NumActions)
		}
	}

	actions := make([]ActionStrategy, game.NumActions)
	for i, freq := range strategy {
		approxEV := (freq - 1.0/float64(game.NumActions)) * req.PotSize * 0.1
		actions[i] = ActionStrategy{
			Action:    game.ActionNames[i],
			Frequency: math.Round(freq*1000) / 1000,
			EV:        math.Round(approxEV*10) / 10,
		}
	}
	jsonResp(w, GTOResponse{Strategy: actions, Bucket: bucket})
}

type RangeUpdateRequest struct {
	HoleCards  []string  `json:"hole_cards"`
	BoardCards []string  `json:"board_cards"`
	ActionType string    `json:"action_type"`
	Weights    []float64 `json:"weights"`
	Position   int       `json:"position"`
	PotSize    float64   `json:"pot_size"`
	StackSize  float64   `json:"stack_size"`
	Street     int       `json:"street"`
}

type RangeShardRequest struct {
	BoardCards []string  `json:"board_cards"`
	ActionType string    `json:"action_type"`
	Weights    []float64 `json:"weights"`
	ComboStart int       `json:"combo_start"`
	ComboEnd   int       `json:"combo_end"`
	PotSize    float64   `json:"pot_size"`
	Street     int       `json:"street"`
}

type RangeShardResponse struct {
	WorkerID       int       `json:"worker_id"`
	UpdatedWeights []float64 `json:"updated_weights"`
}

type DeviationInfo struct {
	Detected     bool    `json:"detected"`
	Severity     string  `json:"severity"`
	GTOFrequency float64 `json:"gto_frequency"`
	ExploitEV    float64 `json:"exploit_ev"`
}

type RangeUpdateResponse struct {
	Weights   []float64      `json:"weights"`
	Stats     any            `json:"stats"`
	Deviation *DeviationInfo `json:"deviation,omitempty"`
}

func (c *Coordinator) httpRangeUpdate(req RangeUpdateRequest, weights []float64) ([]float64, error) {
	const allCombos = 1326
	numWorkers := len(c.cfg.WorkerAddrs)
	shardSize := (allCombos + numWorkers - 1) / numWorkers

	type shardResult struct {
		start   int
		updated []float64
		err     error
	}
	shardResults := make([]shardResult, numWorkers)
	var wg sync.WaitGroup

	for workerIdx, addr := range c.cfg.WorkerAddrs {
		workerIdx, addr := workerIdx, addr
		start := workerIdx * shardSize
		end := min(start+shardSize, allCombos)
		if start >= allCombos {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			shardReq := RangeShardRequest{
				BoardCards: req.BoardCards, ActionType: req.ActionType,
				Weights: weights[start:end], ComboStart: start, ComboEnd: end,
				PotSize: req.PotSize, Street: req.Street,
			}
			var resp RangeShardResponse
			err := postJSON("http://"+addr+"/range/shard", shardReq, &resp)
			shardResults[workerIdx] = shardResult{start: start, updated: resp.UpdatedWeights, err: err}
		}()
	}
	wg.Wait()

	updated := make([]float64, allCombos)
	var firstErr error
	for _, sr := range shardResults {
		if sr.err != nil {
			log.Printf("range shard error: %v", sr.err)
			if firstErr == nil {
				firstErr = sr.err
			}
			end := min(sr.start+shardSize, allCombos)
			copy(updated[sr.start:end], weights[sr.start:end])
			continue
		}
		copy(updated[sr.start:], sr.updated)
	}
	return updated, firstErr
}

func (c *Coordinator) handleRangeUpdate(w http.ResponseWriter, r *http.Request) {
	var req RangeUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	const allCombos = 1326
	all := deck.All1326()

	blocked := util.ParseCards(req.HoleCards)
	blocked = append(blocked, util.ParseCards(req.BoardCards)...)

	actionType := util.ParseActionType(req.ActionType)

	weights := req.Weights
	if len(weights) != allCombos {
		weights = make([]float64, allCombos)
		for i := range weights {
			if req.Street == 0 {
				weights[i] = 1.0
			} else {
				weights[i] = game.OpeningRange(req.Position, game.PreflopHandRank(all[i]))
			}
		}
	}

	var updated []float64

	if req.Street == 0 {
		updated = make([]float64, allCombos)
		for i := range weights {
			rank := game.PreflopHandRank(all[i])
			updated[i] = weights[i] * game.PreflopActionLikelihood(actionType, req.Position, rank)
		}
	} else {

		numWorkers := len(c.cfg.WorkerAddrs)
		shardSize := (allCombos + numWorkers - 1) / numWorkers

		kafkaOK := false
		if c.kafkaWriters != nil {
			requestID := fmt.Sprintf("%d", time.Now().UnixNano())
			pr := &pendingRange{
				ch:       make(chan pkg_kafka.RangeResultMsg, numWorkers),
				expected: numWorkers,
			}
			c.pending.Store(requestID, pr)
			defer c.pending.Delete(requestID)

			publishErr := false
			for workerIdx := range numWorkers {
				start := workerIdx * shardSize
				end := min(start+shardSize, allCombos)
				if start >= allCombos {
					continue
				}
				msg := pkg_kafka.RangeRequestMsg{
					RequestID:  requestID,
					BoardCards: req.BoardCards,
					ActionType: req.ActionType,
					Weights:    weights[start:end],
					ComboStart: start,
					ComboEnd:   end,
					PotSize:    req.PotSize,
					Street:     req.Street,
				}
				if err := pkg_kafka.Publish(r.Context(), c.kafkaWriters[workerIdx], msg); err != nil {
					log.Printf("kafka publish worker%d error: %v — falling back to HTTP", workerIdx+1, err)
					publishErr = true
					break
				}
			}

			if !publishErr {
				kafkaUpdated := make([]float64, allCombos)
				allReceived := true
				for i := 0; i < numWorkers; i++ {
					select {
					case result := <-pr.ch:
						copy(kafkaUpdated[result.ComboStart:], result.UpdatedWeights)
					case <-time.After(45 * time.Second):
						log.Printf("kafka result timeout for shard %d of request %s — falling back to HTTP", i, requestID)
						allReceived = false
						break
					}
					if !allReceived {
						break
					}
				}
				if allReceived {
					updated = kafkaUpdated
					kafkaOK = true
				}
			}
		}

		if !kafkaOK {
			var err error
			updated, err = c.httpRangeUpdate(req, weights)
			if err != nil {
				log.Printf("HTTP range update error: %v", err)
			}
		}

	}

	for i, combo := range all {
		if combo.BlockedBy(blocked) {
			updated[i] = 0
		}
	}

	total := 0.0
	for _, wt := range updated {
		total += wt
	}
	if total > 0 {
		for i := range updated {
			updated[i] /= total
		}
	}

	deviation := detectDeviation(c.table, req.Street, int(req.PotSize), int(req.StackSize), actionType)

	jsonResp(w, RangeUpdateResponse{
		Weights:   updated,
		Stats:     map[string]any{"num_active": countActive(updated)},
		Deviation: deviation,
	})
}

func detectDeviation(table *cfr.StrategyTable, street, pot, stack int, action game.ActionType) *DeviationInfo {
	key := game.InfoSetKey{
		Street: street, Bucket: 9, Position: 3,
		PotSize: game.NearestPotSize(pot), StackDepth: game.NearestStackDepth(stack),
	}
	e, ok := table.Get(key.String())
	if !ok {
		return nil
	}
	avg := e.AverageStrategy()
	if int(action) >= len(avg) {
		return nil
	}
	gtoFreq := avg[int(action)]
	if gtoFreq >= 0.15 {
		return nil
	}
	severity := "low"
	if gtoFreq < 0.05 {
		severity = "high"
	} else if gtoFreq < 0.10 {
		severity = "medium"
	}
	return &DeviationInfo{
		Detected:     true,
		Severity:     severity,
		GTOFrequency: math.Round(gtoFreq*1000) / 1000,
		ExploitEV:    math.Round((0.15-gtoFreq)*2.0*float64(pot)*10) / 10,
	}
}

func countActive(weights []float64) int {
	n := 0
	for _, w := range weights {
		if w > 0 {
			n++
		}
	}
	return n
}

type BRRequest struct {
	HoleCards    []string  `json:"hole_cards"`
	BoardCards   []string  `json:"board_cards"`
	Position     int       `json:"position"`
	PotSize      float64   `json:"pot_size"`
	StackSize    float64   `json:"stack_size"`
	Street       int       `json:"street"`
	RangeWeights []float64 `json:"range_weights"`
}

type BRShardRequest struct {
	HeroIndex       int       `json:"hero_index"`
	OpponentWeights []float64 `json:"opponent_weights"`
	ComboStart      int       `json:"combo_start"`
	ComboEnd        int       `json:"combo_end"`
	BoardCards      []string  `json:"board_cards"`
	PotSize         float64   `json:"pot_size"`
	StackSize       float64   `json:"stack_size"`
	Street          int       `json:"street"`
}

type BRShardResponse struct {
	WorkerID    int     `json:"worker_id"`
	WinWeight   float64 `json:"win_weight"`
	TotalWeight float64 `json:"total_weight"`
}

type BRResponse struct {
	Actions    []ActionStrategy `json:"actions"`
	BestAction string           `json:"best_action"`
	EVGain     float64          `json:"ev_gain"`
}

func (c *Coordinator) handleBestResponse(w http.ResponseWriter, r *http.Request) {
	var req BRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	hole := util.ParseCards(req.HoleCards)
	board := util.ParseCards(req.BoardCards)

	if len(hole) < 2 {
		http.Error(w, "need 2 hole cards", 400)
		return
	}
	heroIndex := deck.Combo{C1: hole[0], C2: hole[1]}.Index()

	const allCombos = 1326
	oppWeights := req.RangeWeights
	if len(oppWeights) != allCombos {
		oppWeights = make([]float64, allCombos)
		for i := range oppWeights {
			oppWeights[i] = 1.0 / float64(allCombos)
		}
	}

	numWorkers := len(c.cfg.WorkerAddrs)
	shardSize := (allCombos + numWorkers - 1) / numWorkers

	type brResult struct {
		winWeight   float64
		totalWeight float64
		err         error
	}
	brResults := make([]brResult, numWorkers)
	var wg sync.WaitGroup

	for workerIdx, addr := range c.cfg.WorkerAddrs {
		workerIdx, addr := workerIdx, addr
		start := workerIdx * shardSize
		end := min(start+shardSize, allCombos)
		wg.Add(1)
		go func() {
			defer wg.Done()
			shardReq := BRShardRequest{
				HeroIndex:       heroIndex,
				OpponentWeights: oppWeights[start:end],
				ComboStart:      start,
				ComboEnd:        end,
				BoardCards:      req.BoardCards,
				PotSize:         req.PotSize,
				StackSize:       req.StackSize,
				Street:          req.Street,
			}
			var resp BRShardResponse
			err := postJSON("http://"+addr+"/br/shard", shardReq, &resp)
			brResults[workerIdx] = brResult{winWeight: resp.WinWeight, totalWeight: resp.TotalWeight, err: err}
		}()
	}
	wg.Wait()

	totalWin, totalWeight := 0.0, 0.0
	for _, res := range brResults {
		if res.err != nil {
			log.Printf("br shard error: %v", res.err)
			continue
		}
		totalWin += res.winWeight
		totalWeight += res.totalWeight
	}
	equity := 0.5
	if totalWeight > 0 {
		equity = totalWin / totalWeight
	}

	evs := brEVsFromEquity(equity, req.PotSize, req.StackSize)

	gtoStrategy := c.gtoStrategyFor(hole, board, req.Position, req.Street, req.PotSize, req.StackSize)
	gtoEV := 0.0
	for i, freq := range gtoStrategy {
		if i < len(evs) {
			gtoEV += freq * evs[i]
		}
	}

	bestIdx, bestEV := 0, math.Inf(-1)
	for i, ev := range evs {
		if ev > bestEV {
			bestEV = ev
			bestIdx = i
		}
	}

	actions := make([]ActionStrategy, game.NumActions)
	for i, ev := range evs {
		actions[i] = ActionStrategy{Action: game.ActionNames[i], EV: math.Round(ev*100) / 100}
	}
	jsonResp(w, BRResponse{
		Actions:    actions,
		BestAction: game.ActionNames[bestIdx],
		EVGain:     math.Round((bestEV-gtoEV)*100) / 100,
	})
}

func brEVsFromEquity(equity, pot, stack float64) []float64 {
	stackToPot := 3.0
	if pot > 0 {
		stackToPot = stack / pot
	}
	evs := game.PostflopActionEVs(equity, stackToPot)
	for i := range evs {
		evs[i] *= pot
	}
	return evs
}

func (c *Coordinator) gtoStrategyFor(hole, board []deck.Card, position, street int, pot, stack float64) []float64 {
	bucket := 0
	if street == 0 && len(hole) == 2 {
		bucket = c.abstraction.PreflopBucket(deck.Combo{C1: hole[0], C2: hole[1]}, position)
	} else if street > 0 && len(hole) == 2 {
		bucket = c.abstraction.PostflopBucket(hole, board, position >= 2)
	}
	texBucket := 0
	if len(board) > 0 {
		texBucket = game.TexBucket(game.ClassifyBoard(board))
	}
	key := game.InfoSetKey{
		Street: street, Bucket: bucket, Position: position,
		PotSize: game.NearestPotSize(int(pot)), StackDepth: game.NearestStackDepth(int(stack)),
		BoardTexBucket: texBucket,
	}
	if e, ok := c.table.Get(key.String()); ok {
		return e.AverageStrategy()
	}
	s := make([]float64, game.NumActions)
	for i := range s {
		s[i] = 1.0 / float64(game.NumActions)
	}
	return s
}

func (c *Coordinator) handleProgress(w http.ResponseWriter, r *http.Request) {
	solved := atomic.LoadInt32(&c.solved)
	total := atomic.LoadInt32(&c.total)
	pct := 0.0
	if total > 0 {
		pct = math.Round(float64(solved)/float64(total)*1000) / 10
	}
	jsonResp(w, map[string]any{"solved": solved, "total": total, "pct": pct})
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func postJSON(url string, req, resp any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	r, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer r.Body.Close()
	if r.StatusCode >= 400 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("%s returned %d: %s", url, r.StatusCode, b)
	}
	return json.NewDecoder(r.Body).Decode(resp)
}

func jsonResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func partitionStrings(ss []string, n int) [][]string {
	if n <= 0 {
		return nil
	}
	size := (len(ss) + n - 1) / n
	parts := make([][]string, n)
	for i := range parts {
		start := i * size
		end := min(start+size, len(ss))
		if start < len(ss) {
			parts[i] = ss[start:end]
		}
	}
	return parts
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	cfg := configFromEnv()
	coord := NewCoordinator(cfg)

	coord.initKafka()
	go coord.runResultConsumer(context.Background())

	go coord.loadOrBuild()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/gto", coord.handleGTO)
	mux.HandleFunc("/api/range/update", coord.handleRangeUpdate)
	mux.HandleFunc("/api/bestresponse", coord.handleBestResponse)
	mux.HandleFunc("/api/progress", coord.handleProgress)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("Coordinator listening on :%s with %d workers", cfg.Port, len(cfg.WorkerAddrs))
	log.Fatal(http.ListenAndServe(":"+cfg.Port, corsMiddleware(mux)))
}
