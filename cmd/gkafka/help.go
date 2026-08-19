package main

const helpText = `gkafka - inspect and exchange Kafka messages

Usage:
  gkafka <command> [options]

Commands:
  ping       Check broker connection and metadata access
  topics     List visible topics
  info       Show topic partitions, offsets, and leaders
  consume    Read a partition without joining a consumer group
  produce    Send one explicitly supplied message (use with caution)
  version    Show version information

Run "gkafka <command> --help" for command options.
`

func commandHelp(command string) string {
	switch command {
	case "ping":
		return `gkafka ping

Usage:
  gkafka ping -b BROKER[,BROKER...]

Options:
  -b, --broker <list>       Comma-separated Kafka brokers
      --timeout <seconds>   Network and metadata timeout (default: 10)
  -h, --help                Show help
`
	case "topics":
		return `gkafka topics

Usage:
  gkafka topics -b BROKER[,BROKER...]

Options:
  -b, --broker <list>       Comma-separated Kafka brokers
      --timeout <seconds>   Network and metadata timeout (default: 10)
  -h, --help                Show help
`
	case "info":
		return `gkafka info

Usage:
  gkafka info -b BROKER[,BROKER...] -t TOPIC

Options:
  -b, --broker <list>       Comma-separated Kafka brokers
  -t, --topic <topic>       Topic name
      --timeout <seconds>   Network and metadata timeout (default: 10)
  -h, --help                Show help
`
	case "consume":
		return `gkafka consume

Usage:
  gkafka consume -b BROKER[,BROKER...] -t TOPIC -p PARTITION [options]

This command reads one partition directly. It never joins a consumer group and
never commits offsets. The default starts at newest and reads at most 10 values.

Options:
  -b, --broker <list>       Comma-separated Kafka brokers
  -t, --topic <topic>       Topic name
  -p, --partition <id>      Partition ID (default: 0)
  -n, --max <count>         Maximum messages (default: 10)
      --offset <position>   newest, oldest, or numeric offset (default: newest)
      --timeout <seconds>   Total wait limit (default: 60)
      --meta                Write message metadata to stderr
      --escape              Escape non-printable stdout bytes as \xNN
  -o, --output <file>       Write original values to a file instead of stdout
  -h, --help                Show help

RAW output concatenates message values without adding separators. This preserves
every value byte but does not preserve boundaries between multiple messages.
Use --meta to record offsets and sizes. --escape never changes --output files.
`
	case "produce":
		return `gkafka produce

WARNING: producing to an operational topic changes external state.

Usage:
  gkafka produce -b BROKER[,BROKER...] -t TOPIC -m MESSAGE

Options:
  -b, --broker <list>       Comma-separated Kafka brokers
  -t, --topic <topic>       Destination topic
  -m, --message <value>     Message value to send
      --timeout <seconds>   Network timeout (default: 10)
  -h, --help                Show help
`
	default:
		return helpText
	}
}
