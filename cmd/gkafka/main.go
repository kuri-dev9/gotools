package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	gkafka "gtools/pkg/kafka"
	"gtools/pkg/version"

	"github.com/Shopify/sarama"
	"github.com/spf13/pflag"
)

const versionInfo = "0.1.0"

type commonOptions struct {
	broker  string
	timeout int
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(helpText)
		return nil
	}
	switch args[0] {
	case "version", "-v", "--version":
		version.Version = versionInfo
		version.Print()
		return nil
	case "ping":
		return runPing(args[1:])
	case "topics":
		return runTopics(args[1:])
	case "info":
		return runInfo(args[1:])
	case "consume":
		return runConsume(args[1:])
	case "produce":
		return runProduce(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func newFlags(command string, defaultTimeout int) (*pflag.FlagSet, *commonOptions, *bool) {
	flags := pflag.NewFlagSet("gkafka "+command, pflag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := &commonOptions{}
	flags.StringVarP(&options.broker, "broker", "b", "", "comma-separated Kafka brokers")
	flags.IntVar(&options.timeout, "timeout", defaultTimeout, "timeout seconds")
	help := flags.BoolP("help", "h", false, "show help")
	flags.Usage = func() { fmt.Print(commandHelp(command)) }
	return flags, options, help
}

func parseCommon(flags *pflag.FlagSet, options *commonOptions, help *bool, args []string) ([]string, time.Duration, bool, error) {
	if err := flags.Parse(args); err != nil {
		return nil, 0, false, err
	}
	if *help {
		flags.Usage()
		return nil, 0, true, nil
	}
	if len(flags.Args()) != 0 {
		return nil, 0, false, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if options.broker == "" {
		return nil, 0, false, fmt.Errorf("--broker is required")
	}
	if options.timeout <= 0 {
		return nil, 0, false, fmt.Errorf("--timeout must be greater than zero")
	}
	brokers, err := gkafka.ParseBrokers(options.broker)
	if err != nil {
		return nil, 0, false, err
	}
	return brokers, time.Duration(options.timeout) * time.Second, false, nil
}

func openClient(brokers []string, timeout time.Duration) (sarama.Client, error) {
	return gkafka.NewClient(gkafka.Config{Brokers: brokers, Timeout: timeout})
}

func runPing(args []string) error {
	flags, options, help := newFlags("ping", 10)
	brokers, timeout, done, err := parseCommon(flags, options, help, args)
	if err != nil || done {
		return err
	}
	client, err := openClient(brokers, timeout)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.RefreshMetadata(); err != nil {
		return fmt.Errorf("request Kafka metadata: %w", err)
	}
	known := client.Brokers()
	fmt.Println("Status  : OK")
	fmt.Printf("Brokers : %d\n", len(known))
	for _, broker := range known {
		fmt.Printf("- ID=%d Address=%s\n", broker.ID(), broker.Addr())
	}
	return nil
}

func runTopics(args []string) error {
	flags, options, help := newFlags("topics", 10)
	brokers, timeout, done, err := parseCommon(flags, options, help, args)
	if err != nil || done {
		return err
	}
	client, err := openClient(brokers, timeout)
	if err != nil {
		return err
	}
	defer client.Close()
	topics, err := gkafka.Topics(client)
	if err != nil {
		return err
	}
	for _, topic := range topics {
		fmt.Println(topic)
	}
	return nil
}

func runInfo(args []string) error {
	flags, options, help := newFlags("info", 10)
	topic := flags.StringP("topic", "t", "", "topic name")
	brokers, timeout, done, err := parseCommon(flags, options, help, args)
	if err != nil || done {
		return err
	}
	if *topic == "" {
		return fmt.Errorf("--topic is required")
	}
	client, err := openClient(brokers, timeout)
	if err != nil {
		return err
	}
	defer client.Close()
	info, err := gkafka.Info(client, *topic)
	if err != nil {
		return err
	}
	fmt.Printf("Topic      : %s\n", info.Name)
	fmt.Printf("Partitions : %d\n", len(info.Partitions))
	for _, partition := range info.Partitions {
		fmt.Printf("Partition %d: oldest=%d newest=%d leader=%d address=%s\n", partition.ID, partition.OldestOffset, partition.NewestOffset, partition.LeaderID, partition.LeaderAddr)
	}
	return nil
}

func runConsume(args []string) error {
	flags, options, help := newFlags("consume", 60)
	topic := flags.StringP("topic", "t", "", "topic name")
	partition := flags.Int32P("partition", "p", 0, "partition ID")
	maximum := flags.IntP("max", "n", 10, "maximum messages")
	offsetValue := flags.String("offset", "newest", "newest, oldest, or numeric offset")
	output := flags.StringP("output", "o", "", "raw output file")
	meta := flags.Bool("meta", false, "write metadata to stderr")
	escape := flags.Bool("escape", false, "escape non-printable stdout bytes")
	brokers, timeout, done, err := parseCommon(flags, options, help, args)
	if err != nil || done {
		return err
	}
	if *topic == "" {
		return fmt.Errorf("--topic is required")
	}
	if *partition < 0 {
		return fmt.Errorf("--partition must not be negative")
	}
	if *maximum <= 0 {
		return fmt.Errorf("--max must be greater than zero")
	}
	offset, err := gkafka.ParseOffset(*offsetValue)
	if err != nil {
		return err
	}
	client, err := openClient(brokers, timeout)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := gkafka.ValidatePartition(client, *topic, *partition); err != nil {
		return err
	}
	consumer, err := sarama.NewConsumerFromClient(client)
	if err != nil {
		return fmt.Errorf("create Kafka consumer: %w", err)
	}
	defer consumer.Close()
	partitionConsumer, err := consumer.ConsumePartition(*topic, *partition, offset)
	if err != nil {
		return fmt.Errorf("consume topic %q partition %d: %w", *topic, *partition, err)
	}
	defer partitionConsumer.Close()

	writer, closeWriter, err := outputWriter(*output)
	if err != nil {
		return err
	}
	defer closeWriter()

	ctx, cancel := signalContext(timeout)
	defer cancel()
	count := 0
	for count < *maximum {
		select {
		case <-ctx.Done():
			return nil
		case consumerErr, ok := <-partitionConsumer.Errors():
			if ok && consumerErr != nil {
				return fmt.Errorf("Kafka consumer error: %w", consumerErr.Err)
			}
		case message, ok := <-partitionConsumer.Messages():
			if !ok {
				return nil
			}
			if *meta {
				writeMeta(os.Stderr, message)
			}
			if *escape && *output == "" {
				err = gkafka.WriteEscaped(writer, message.Value)
			} else {
				_, err = writer.Write(message.Value)
			}
			if err != nil {
				return fmt.Errorf("write message output: %w", err)
			}
			count++
		}
	}
	return nil
}

func outputWriter(filename string) (io.Writer, func() error, error) {
	if filename == "" {
		return os.Stdout, func() error { return nil }, nil
	}
	file, err := os.Create(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("create output file %q: %w", filename, err)
	}
	return file, file.Close, nil
}

func signalContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	parent, stop := context.WithTimeout(context.Background(), timeout)
	ctx, cancel := context.WithCancel(parent)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-signals:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(signals)
		stop()
	}()
	return ctx, cancel
}

func writeMeta(writer io.Writer, message *sarama.ConsumerMessage) {
	timestamp := "unknown"
	if !message.Timestamp.IsZero() {
		timestamp = message.Timestamp.Format(time.RFC3339Nano)
	}
	fmt.Fprintf(writer, "Topic=%s Partition=%d Offset=%d Timestamp=%s Key=%x Size=%d\n", message.Topic, message.Partition, message.Offset, timestamp, message.Key, len(message.Value))
}

func runProduce(args []string) error {
	flags, options, help := newFlags("produce", 10)
	topic := flags.StringP("topic", "t", "", "destination topic")
	message := flags.StringP("message", "m", "", "message value")
	brokers, timeout, done, err := parseCommon(flags, options, help, args)
	if err != nil || done {
		return err
	}
	if *topic == "" {
		return fmt.Errorf("--topic is required")
	}
	if !flags.Changed("message") {
		return fmt.Errorf("--message is required")
	}
	config, err := gkafka.SaramaConfig(timeout)
	if err != nil {
		return err
	}
	producer, err := sarama.NewSyncProducer(brokers, config)
	if err != nil {
		return fmt.Errorf("create Kafka producer: %w", err)
	}
	defer producer.Close()
	partition, offset, err := producer.SendMessage(&sarama.ProducerMessage{Topic: *topic, Value: sarama.ByteEncoder([]byte(*message))})
	if err != nil {
		return fmt.Errorf("produce message to topic %q: %w", *topic, err)
	}
	fmt.Printf("Topic=%s Partition=%d Offset=%d\n", *topic, partition, offset)
	return nil
}
