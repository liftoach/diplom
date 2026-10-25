package transport

import (
	"context"
	"strings"
	"time"

	"diplom/internal/platform"

	"github.com/segmentio/kafka-go"
)

func Brokers() []string { return strings.Split(platform.Env("KAFKA_BROKERS", "localhost:9092"), ",") }
func Writer() *kafka.Writer {
	return &kafka.Writer{Addr: kafka.TCP(Brokers()...), Balancer: &kafka.Hash{}, RequiredAcks: kafka.RequireAll, Async: false, WriteTimeout: 8 * time.Second, ReadTimeout: 8 * time.Second, BatchTimeout: 10 * time.Millisecond, MaxAttempts: 3}
}
func Ready(ctx context.Context) error {
	conn, err := kafka.DialContext(ctx, "tcp", Brokers()[0])
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	_, err = conn.ReadPartitions(platform.Env("KAFKA_TOPIC", "telemetry.valid.v1"))
	return err
}
