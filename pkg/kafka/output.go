package kafka

import (
	"fmt"
	"io"
)

func WriteEscaped(writer io.Writer, value []byte) error {
	for _, current := range value {
		if current >= 0x20 && current <= 0x7e {
			if _, err := writer.Write([]byte{current}); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(writer, "\\x%02x", current); err != nil {
			return err
		}
	}
	return nil
}
