package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

func writeResult(writer io.Writer, format, text string, value any) error {
	switch format {
	case "text":
		_, err := fmt.Fprintln(writer, text)
		return err
	case "json":
		encoder := json.NewEncoder(writer)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(value)
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
}

func writeNDJSON(writer io.Writer, values ...any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	for _, value := range values {
		if err := encoder.Encode(value); err != nil {
			return fmt.Errorf("encode NDJSON: %w", err)
		}
	}
	return nil
}
