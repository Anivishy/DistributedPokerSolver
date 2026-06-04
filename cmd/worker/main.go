package main

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"

	"poker-solver/pkg/cfr"
	"poker-solver/pkg/deck"
	"poker-solver/pkg/game"
	pkg_kafka "poker-solver/pkg/kafka"
	"poker-solver/pkg/util"
)

type Worker struct {
	ID     int
	table  *cfr.StrategyTable
	solver *cfr.Solver
}

func NewWorker(id int) *Worker {
	table := cfr.NewStrategyTable(game.NumActions)
	solver := cfr.NewSolver(table, nil)
	return &Worker{ID: id, table: table, solver: solver}
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

func (w *Worker) handlePreflopShard(rw http.ResponseWriter, r *http.Request) {
	var req PreflopShardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}
	if req.NumSamples <= 0 {
		req.NumSamples = 15
	}
	all := deck.All1326()
	size := req.ComboEnd - req.ComboStart
	if size <= 0 {
		jsonResp(rw, PreflopShardResponse{WorkerID: w.ID})
		return
	}
	equities := make([]float64, size)
	for i := range size {
		idx := req.ComboStart + i
		if idx >= len(all) {
			break
		}
		combo := all[idx]
		equities[i] = game.Equity([]deck.Card{combo.C1, combo.C2}, nil, req.NumSamples)
	}
	jsonResp(rw, PreflopShardResponse{WorkerID: w.ID, Equities: equities})
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

func (w *Worker) handleCFRShard(rw http.ResponseWriter, r *http.Request) {
	var req CFRShardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}
	updates := w.solver.TraverseInfoSets(req.Keys, req.Strategies, req.Iteration)
	jsonResp(rw, CFRShardResponse{WorkerID: w.ID, Updates: updates})
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
	ShardEquity    float64   `json:"shard_equity"`
}

func (w *Worker) handleRangeShard(rw http.ResponseWriter, r *http.Request) {
	var req RangeShardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}
	board := util.ParseCards(req.BoardCards)
	actionType := util.ParseActionType(req.ActionType)
	all := deck.All1326()

	updated := make([]float64, len(req.Weights))
	totalEquity, totalWeight := 0.0, 0.0

	for i, weight := range req.Weights {
		comboIdx := req.ComboStart + i
		if comboIdx >= req.ComboEnd || comboIdx >= len(all) {
			break
		}
		if weight == 0 {
			continue
		}
		combo := all[comboIdx]
		samples := 4
		if len(board) == 0 {
			samples = 6
		}
		comboCards := []deck.Card{combo.C1, combo.C2}
		eq := game.Equity(comboCards, board, samples)
		draw := game.DrawStrength(comboCards, board)
		likelihood := util.ActionLikelihoodWithDraw(actionType, eq, draw)
		updated[i] = weight * likelihood
		totalEquity += eq * updated[i]
		totalWeight += updated[i]
	}

	avgEquity := 0.0
	if totalWeight > 0 {
		avgEquity = totalEquity / totalWeight
	}
	jsonResp(rw, RangeShardResponse{
		WorkerID:       w.ID,
		UpdatedWeights: updated,
		ShardEquity:    math.Round(avgEquity*1000) / 1000,
	})
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

func (w *Worker) handleBRShard(rw http.ResponseWriter, r *http.Request) {
	var req BRShardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(rw, err.Error(), 400)
		return
	}

	all := deck.All1326()
	if req.HeroIndex < 0 || req.HeroIndex >= len(all) {
		jsonResp(rw, BRShardResponse{WorkerID: w.ID})
		return
	}
	hero := all[req.HeroIndex]
	heroCards := []deck.Card{hero.C1, hero.C2}
	board := util.ParseCards(req.BoardCards)

	const brSamples = 200

	winWeight, totalWeight := 0.0, 0.0

	for i, wt := range req.OpponentWeights {
		if wt <= 0 {
			continue
		}
		oppIdx := req.ComboStart + i
		if oppIdx >= req.ComboEnd || oppIdx >= len(all) {
			break
		}
		opp := all[oppIdx]
		if opp.BlockedBy(heroCards) || opp.BlockedBy(board) {
			continue
		}

		heroWinProb := game.HeadsUpEquity(heroCards, []deck.Card{opp.C1, opp.C2}, board, brSamples)
		winWeight += heroWinProb * wt
		totalWeight += wt
	}

	jsonResp(rw, BRShardResponse{
		WorkerID:    w.ID,
		WinWeight:   winWeight,
		TotalWeight: totalWeight,
	})
}

func (w *Worker) runKafkaConsumer(kafkaAddr string) {
	topicIdx := w.ID - 1
	reader := pkg_kafka.NewReader(kafkaAddr, pkg_kafka.RangeRequestTopic(topicIdx))
	writer := pkg_kafka.NewWriter(kafkaAddr, pkg_kafka.TopicRangeResults)

	log.Printf("Worker %d: Kafka consumer started (topic=%s, results→%s)",
		w.ID, pkg_kafka.RangeRequestTopic(topicIdx), pkg_kafka.TopicRangeResults)

	ctx := context.Background()
	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			log.Printf("Worker %d: kafka read error: %v", w.ID, err)
			continue
		}

		var req pkg_kafka.RangeRequestMsg
		if err := pkg_kafka.Decode(msg, &req); err != nil {
			log.Printf("Worker %d: kafka decode error: %v", w.ID, err)
			continue
		}

		board := util.ParseCards(req.BoardCards)
		actionType := util.ParseActionType(req.ActionType)
		all := deck.All1326()

		updated := make([]float64, len(req.Weights))
		totalEquity, totalWeight := 0.0, 0.0

		samples := 4
		if len(board) == 0 {
			samples = 6
		}
		for i, weight := range req.Weights {
			comboIdx := req.ComboStart + i
			if comboIdx >= req.ComboEnd || comboIdx >= len(all) {
				break
			}
			if weight == 0 {
				continue
			}
			combo := all[comboIdx]
			comboCards := []deck.Card{combo.C1, combo.C2}
			eq := game.Equity(comboCards, board, samples)
			draw := game.DrawStrength(comboCards, board)
			likelihood := util.ActionLikelihoodWithDraw(actionType, eq, draw)
			updated[i] = weight * likelihood
			totalEquity += eq * updated[i]
			totalWeight += updated[i]
		}
		_ = totalEquity
		_ = totalWeight

		result := pkg_kafka.RangeResultMsg{
			RequestID:      req.RequestID,
			WorkerID:       w.ID,
			ComboStart:     req.ComboStart,
			UpdatedWeights: updated,
		}
		if err := pkg_kafka.Publish(ctx, writer, result); err != nil {
			log.Printf("Worker %d: kafka publish result error: %v", w.ID, err)
		}
	}
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

func main() {
	idStr := os.Getenv("WORKER_ID")
	id, _ := strconv.Atoi(idStr)
	port := os.Getenv("PORT")
	if port == "" {
		port = "9001"
	}

	wk := NewWorker(id)

	if kafkaAddr := os.Getenv("KAFKA_ADDR"); kafkaAddr != "" {
		go wk.runKafkaConsumer(kafkaAddr)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/preflop/shard", wk.handlePreflopShard)
	mux.HandleFunc("/cfr/shard", wk.handleCFRShard)
	mux.HandleFunc("/range/shard", wk.handleRangeShard)
	mux.HandleFunc("/br/shard", wk.handleBRShard)
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.Write([]byte(`{"status":"ok","worker_id":` + idStr + `}`))
	})

	log.Printf("Worker %d listening on :%s", id, port)
	log.Fatal(http.ListenAndServe(":"+port, corsMiddleware(mux)))
}
