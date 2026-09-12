package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
)

// Result is what a tool hands back.
//
// Every result carries both a human readable rendering and, where it makes
// sense, a structured payload. The MCP specification asks servers that return
// structured content to also return its text form, and in practice models
// answer better from a compact table than from a wall of JSON.
type Result struct {
	// Text is the rendering shown to the model and to anyone reading the
	// conversation.
	Text string
	// Structured is an optional JSON-serialisable payload.
	Structured any
	// Err marks the result as a tool error rather than a protocol error.
	Err error
}

// Text builds a plain text result.
func Text(text string) *Result {
	return &Result{Text: text}
}

// Textf builds a formatted plain text result.
func Textf(format string, args ...any) *Result {
	return &Result{Text: fmt.Sprintf(format, args...)}
}

// Structured builds a result with both a text rendering and a payload.
func Structured(text string, payload any) *Result {
	return &Result{Text: text, Structured: payload}
}

// JSON builds a result whose text rendering is the indented JSON payload.
func JSON(payload any) *Result {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return Error(fmt.Errorf("cannot serialise result: %w", err))
	}
	return &Result{Text: string(encoded), Structured: payload}
}

// Error builds a result that reports a failure to the model.
func Error(err error) *Result {
	return &Result{Err: err}
}

// Errorf builds a formatted error result.
func Errorf(format string, args ...any) *Result {
	return &Result{Err: fmt.Errorf(format, args...)}
}

// Table renders rows as an aligned, kubectl-style table. An empty table still
// prints its header so the model can see which columns exist.
func Table(headers []string, rows [][]string) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	_ = w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

// Section appends a titled block to a builder, skipping empty bodies.
func Section(b *strings.Builder, title, body string) {
	if strings.TrimSpace(body) == "" {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(body)
}
