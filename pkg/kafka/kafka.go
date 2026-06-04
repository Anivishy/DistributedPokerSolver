package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

func RangeRequestTopic(workerIdx int) string {
	return fmt.Sprintf("range-requests-%d", workerIdx)
}

const TopicRangeResults = "range-results"

type RangeRequestMsg struct {
	RequestID  string    `json:"request_id"`
	BoardCards []string  `json:"board_cards"`
	ActionType string    `json:"action_type"`
	Weights    []float64 `json:"weights"`
	ComboStart int       `json:"combo_start"`
	ComboEnd   int       `json:"combo_end"`
	PotSize    float64   `json:"pot_size"`
	Street     int       `json:"street"`
}

type RangeResultMsg struct {
	RequestID      string    `json:"request_id"`
	WorkerID       int       `json:"worker_id"`
	ComboStart     int       `json:"combo_start"`
	UpdatedWeights []float64 `json:"updated_weights"`
}

func NewWriter(broker, topic string) *kafkago.Writer {
	return &kafkago.Writer{
		Addr:                   kafkago.TCP(broker),
		Topic:                  topic,
		AllowAutoTopicCreation: true,
		WriteTimeout:           5 * time.Second,
	}
}

func NewReader(broker, topic string) *kafkago.Reader {
	return kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     []string{broker},
		Topic:       topic,
		StartOffset: kafkago.LastOffset,
		MinBytes:    1,
		MaxBytes:    int(10e6),
	})
}

func Publish(ctx context.Context, w *kafkago.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("kafka publish marshal: %w", err)
	}
	return w.WriteMessages(ctx, kafkago.Message{Value: data})
}

func Decode(msg kafkago.Message, v any) error {
	return json.Unmarshal(msg.Value, v)
}
