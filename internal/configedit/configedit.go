// Package configedit provides narrow TOML edits that preserve user formatting.
package configedit

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AppendBlock appends a TOML section with the given body lines to path.
func AppendBlock(path, header string, lines []string) error {
	if err := validateHeader(header); err != nil {
		return err
	}
	if err := validateLines(lines); err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading config: %w", err)
	}

	newline := preferredNewline(data)
	var out strings.Builder
	out.Grow(len(data) + len(header) + len(lines)*24 + 8)
	out.Write(data)
	if len(data) > 0 {
		if !bytes.HasSuffix(data, []byte("\n")) {
			out.WriteString(newline)
		}
		if !bytes.HasSuffix(data, []byte(newline+newline)) {
			out.WriteString(newline)
		}
	}
	out.WriteString(formatBlock(header, lines, newline))

	if err := writeFile(path, []byte(out.String())); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// SetField sets key to value in header, inserting the field or section if
// needed. Unchanged lines are written back byte-identically.
func SetField(path, header, key, value string) error {
	if err := validateHeader(header); err != nil {
		return err
	}
	if err := validateKey(key); err != nil {
		return err
	}
	if err := validateValue(value); err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return AppendBlock(path, header, []string{fieldLine(key, value)})
	}
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}
	if len(data) == 0 {
		return AppendBlock(path, header, []string{fieldLine(key, value)})
	}

	lines := splitLines(data)
	start, end := findSection(lines, header)
	if start == -1 {
		return AppendBlock(path, header, []string{fieldLine(key, value)})
	}

	for i := start + 1; i < end; i++ {
		indent, ok := fieldIndent(lines[i].text, key)
		if !ok {
			continue
		}
		lines[i].text = indent + fieldLine(key, value)
		if err := writeFile(path, joinLines(lines)); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		return nil
	}

	newline := preferredLineNewline(lines)
	insertAt := insertionIndex(lines, start, end)
	if insertAt > 0 && lines[insertAt-1].newline == "" {
		lines[insertAt-1].newline = newline
	}
	lines = append(lines, line{})
	copy(lines[insertAt+1:], lines[insertAt:])
	lines[insertAt] = line{text: fieldLine(key, value), newline: newline}

	if err := writeFile(path, joinLines(lines)); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

type line struct {
	text    string
	newline string
}

func splitLines(data []byte) []line {
	if len(data) == 0 {
		return nil
	}
	lines := make([]line, 0, bytes.Count(data, []byte("\n"))+1)
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i == -1 {
			lines = append(lines, line{text: string(data)})
			break
		}
		text := data[:i]
		newline := "\n"
		if len(text) > 0 && text[len(text)-1] == '\r' {
			text = text[:len(text)-1]
			newline = "\r\n"
		}
		lines = append(lines, line{text: string(text), newline: newline})
		data = data[i+1:]
	}
	return lines
}

func joinLines(lines []line) []byte {
	var out strings.Builder
	for _, line := range lines {
		out.WriteString(line.text)
		out.WriteString(line.newline)
	}
	return []byte(out.String())
}

func findSection(lines []line, header string) (int, int) {
	start := -1
	for i, line := range lines {
		name, ok := sectionHeader(line.text)
		if !ok {
			continue
		}
		if start != -1 {
			return start, i
		}
		if name == header {
			start = i
		}
	}
	if start == -1 {
		return -1, -1
	}
	return start, len(lines)
}

func sectionHeader(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	if strings.HasPrefix(trimmed, "[[") {
		end := strings.Index(trimmed, "]]")
		if end == -1 {
			return "", false
		}
		rest := strings.TrimSpace(trimmed[end+2:])
		return "", rest == "" || strings.HasPrefix(rest, "#")
	}
	if !strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	end := strings.IndexByte(trimmed, ']')
	if end <= 0 {
		return "", false
	}
	rest := strings.TrimSpace(trimmed[end+1:])
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return "", false
	}
	name := strings.TrimSpace(trimmed[1:end])
	return name, name != ""
}

func fieldIndent(text, key string) (string, bool) {
	body := strings.TrimLeft(text, " \t")
	if body == "" || strings.HasPrefix(body, "#") || !strings.HasPrefix(body, key) {
		return "", false
	}
	rest := strings.TrimLeft(body[len(key):], " \t")
	if !strings.HasPrefix(rest, "=") {
		return "", false
	}
	return text[:len(text)-len(body)], true
}

func insertionIndex(lines []line, start, end int) int {
	insertAt := end
	for insertAt > start+1 && strings.TrimSpace(lines[insertAt-1].text) == "" {
		insertAt--
	}
	return insertAt
}

func formatBlock(header string, lines []string, newline string) string {
	var out strings.Builder
	out.WriteString("[")
	out.WriteString(header)
	out.WriteString("]")
	out.WriteString(newline)
	for _, line := range lines {
		out.WriteString(line)
		out.WriteString(newline)
	}
	return out.String()
}

func fieldLine(key, value string) string {
	return key + " = " + value
}

func preferredNewline(data []byte) string {
	if bytes.Contains(data, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

func preferredLineNewline(lines []line) string {
	for _, line := range lines {
		if line.newline != "" {
			return line.newline
		}
	}
	return "\n"
}

func validateHeader(header string) error {
	if strings.TrimSpace(header) == "" {
		return fmt.Errorf("header must be non-empty")
	}
	if strings.ContainsAny(header, "\r\n[]") {
		return fmt.Errorf("header %q is not a simple section name", header)
	}
	return nil
}

func validateKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("key must be non-empty")
	}
	if strings.ContainsAny(key, "\r\n=") {
		return fmt.Errorf("key %q is not a simple field name", key)
	}
	return nil
}

func validateLines(lines []string) error {
	for i, line := range lines {
		if strings.ContainsAny(line, "\r\n") {
			return fmt.Errorf("line %d contains a newline", i)
		}
	}
	return nil
}

func validateValue(value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("value contains a newline")
	}
	return nil
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stating config: %w", err)
	}
	return os.WriteFile(path, data, mode)
}
