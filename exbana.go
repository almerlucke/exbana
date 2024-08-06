package exbana

import (
	"bytes"
	"fmt"
	"io"
)

// IsStreamError check if err is set and not io.EOF
func IsStreamError(err error) bool {
	return err != nil && err != io.EOF
}

// Scan stream for pattern and return all results
func Scan[T, P any](stream Reader[T, P], pattern Pattern[T, P]) ([]*Match[T, P], error) {
	var results []*Match[T, P]

	for !stream.Finished() {
		pos, err := stream.Position()
		if IsStreamError(err) {
			return nil, err
		}
		matched, result, err := pattern.Match(stream)
		if err != nil {
			return nil, err
		}
		if matched {
			results = append(results, result)
		} else {
			err = stream.SetPosition(pos)
			if IsStreamError(err) {
				return nil, err
			}
			_, err = stream.Skip(1)
			if IsStreamError(err) {
				return nil, err
			}
		}
	}

	return results, nil
}

// PrintRules prints all rules and returns a string
func PrintRules[T, P any](patterns []Pattern[T, P]) (string, error) {
	var buf bytes.Buffer

	for _, pattern := range patterns {
		_, err := buf.WriteString(fmt.Sprintf("%s = ", pattern.ID()))
		if err != nil {
			return "", err
		}

		err = pattern.Print(&buf)
		if err != nil {
			return "", err
		}

		_, err = buf.WriteString("\n")
		if err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}
