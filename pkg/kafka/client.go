// Package kafka provides read-only Kafka inspection helpers and explicit
// message production without using consumer groups.
package kafka

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Shopify/sarama"
)

const DefaultTimeout = 10 * time.Second

type Config struct {
	Brokers []string
	Timeout time.Duration
}

type TopicInfo struct {
	Name       string
	Partitions []PartitionInfo
}

type PartitionInfo struct {
	ID           int32
	OldestOffset int64
	NewestOffset int64
	LeaderID     int32
	LeaderAddr   string
}

func ParseBrokers(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	brokers := make([]string, 0, len(parts))
	for _, part := range parts {
		broker := strings.TrimSpace(part)
		if broker == "" {
			return nil, fmt.Errorf("broker list contains an empty address")
		}
		host, port, err := net.SplitHostPort(broker)
		if err != nil || strings.TrimSpace(host) == "" {
			return nil, fmt.Errorf("invalid broker %q (expected host:port)", broker)
		}
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return nil, fmt.Errorf("invalid broker port in %q", broker)
		}
		brokers = append(brokers, broker)
	}
	if len(brokers) == 0 {
		return nil, fmt.Errorf("at least one broker is required")
	}
	return brokers, nil
}

func SaramaConfig(timeout time.Duration) (*sarama.Config, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	config := sarama.NewConfig()
	config.ClientID = "gkafka"
	config.Net.DialTimeout = timeout
	config.Net.ReadTimeout = timeout
	config.Net.WriteTimeout = timeout
	config.Metadata.Timeout = timeout
	config.Consumer.Return.Errors = true
	config.Consumer.Offsets.AutoCommit.Enable = false
	config.Consumer.Offsets.Initial = sarama.OffsetNewest
	config.Producer.Return.Successes = true
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid Kafka client configuration: %w", err)
	}
	return config, nil
}

func NewClient(config Config) (sarama.Client, error) {
	saramaConfig, err := SaramaConfig(config.Timeout)
	if err != nil {
		return nil, err
	}
	client, err := sarama.NewClient(config.Brokers, saramaConfig)
	if err != nil {
		return nil, fmt.Errorf("connect to Kafka brokers: %w", err)
	}
	return client, nil
}

func Topics(client sarama.Client) ([]string, error) {
	topics, err := client.Topics()
	if err != nil {
		return nil, fmt.Errorf("request Kafka metadata: %w", err)
	}
	sort.Strings(topics)
	return topics, nil
}

func Info(client sarama.Client, topic string) (TopicInfo, error) {
	if err := EnsureTopic(client, topic); err != nil {
		return TopicInfo{}, err
	}
	partitions, err := client.Partitions(topic)
	if err != nil {
		return TopicInfo{}, fmt.Errorf("get partitions for topic %q: %w", topic, err)
	}
	if len(partitions) == 0 {
		return TopicInfo{}, fmt.Errorf("topic %q has no partitions or does not exist", topic)
	}
	sort.Slice(partitions, func(i, j int) bool { return partitions[i] < partitions[j] })
	info := TopicInfo{Name: topic, Partitions: make([]PartitionInfo, 0, len(partitions))}
	for _, partition := range partitions {
		oldest, err := client.GetOffset(topic, partition, sarama.OffsetOldest)
		if err != nil {
			return TopicInfo{}, fmt.Errorf("get oldest offset for partition %d: %w", partition, err)
		}
		newest, err := client.GetOffset(topic, partition, sarama.OffsetNewest)
		if err != nil {
			return TopicInfo{}, fmt.Errorf("get newest offset for partition %d: %w", partition, err)
		}
		leader, err := client.Leader(topic, partition)
		if err != nil {
			return TopicInfo{}, fmt.Errorf("get leader for partition %d: %w", partition, err)
		}
		info.Partitions = append(info.Partitions, PartitionInfo{
			ID: partition, OldestOffset: oldest, NewestOffset: newest,
			LeaderID: leader.ID(), LeaderAddr: leader.Addr(),
		})
	}
	return info, nil
}

// EnsureTopic checks cluster metadata before issuing a topic-specific request.
// This avoids accidentally triggering broker-side topic auto-creation while
// using read-only commands.
func EnsureTopic(client sarama.Client, topic string) error {
	topics, err := client.Topics()
	if err != nil {
		return fmt.Errorf("request Kafka metadata: %w", err)
	}
	for _, current := range topics {
		if current == topic {
			return nil
		}
	}
	return fmt.Errorf("topic %q not found", topic)
}

func ParseOffset(value string) (int64, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "newest":
		return sarama.OffsetNewest, nil
	case "oldest":
		return sarama.OffsetOldest, nil
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid offset %q (expected newest, oldest, or a non-negative number)", value)
	}
	return offset, nil
}

func ValidatePartition(client sarama.Client, topic string, partition int32) error {
	if err := EnsureTopic(client, topic); err != nil {
		return err
	}
	partitions, err := client.Partitions(topic)
	if err != nil {
		return fmt.Errorf("get partitions for topic %q: %w", topic, err)
	}
	for _, current := range partitions {
		if current == partition {
			return nil
		}
	}
	return fmt.Errorf("invalid partition %d for topic %q", partition, topic)
}
