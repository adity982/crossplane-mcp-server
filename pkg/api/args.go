package api

import (
	"errors"
	"fmt"
	"strings"
)

// Args reads tool arguments out of the raw map the MCP client sent.
//
// Accessors accumulate errors instead of returning them, so a handler can
// read every argument it needs and check once. This keeps handlers readable
// and reports all the mistakes in a call at the same time, rather than making
// the model fix them one round trip at a time.
type Args struct {
	raw  map[string]any
	errs []error
}

// NewArgs wraps a raw argument map. A nil map is valid and behaves as if every
// argument was omitted.
func NewArgs(raw map[string]any) *Args {
	if raw == nil {
		raw = map[string]any{}
	}
	return &Args{raw: raw}
}

// Err returns the accumulated argument errors, or nil.
func (a *Args) Err() error {
	return errors.Join(a.errs...)
}

// String reads a required string argument.
func (a *Args) String(name string) string {
	value, ok := a.raw[name]
	if !ok || value == nil {
		a.fail("missing required argument %q", name)
		return ""
	}
	s, ok := value.(string)
	if !ok {
		a.fail("argument %q must be a string, got %T", name, value)
		return ""
	}
	if strings.TrimSpace(s) == "" {
		a.fail("argument %q must not be empty", name)
		return ""
	}
	return s
}

// OptionalString reads a string argument, falling back to fallback.
func (a *Args) OptionalString(name, fallback string) string {
	value, ok := a.raw[name]
	if !ok || value == nil {
		return fallback
	}
	s, ok := value.(string)
	if !ok {
		a.fail("argument %q must be a string, got %T", name, value)
		return fallback
	}
	if s == "" {
		return fallback
	}
	return s
}

// OptionalEnum reads a string argument and checks it against a closed set.
func (a *Args) OptionalEnum(name, fallback string, allowed ...string) string {
	value := a.OptionalString(name, fallback)
	for _, candidate := range allowed {
		if strings.EqualFold(value, candidate) {
			return candidate
		}
	}
	a.fail("argument %q must be one of %s, got %q", name, strings.Join(allowed, ", "), value)
	return fallback
}

// OptionalBool reads a boolean argument.
func (a *Args) OptionalBool(name string, fallback bool) bool {
	value, ok := a.raw[name]
	if !ok || value == nil {
		return fallback
	}
	b, ok := value.(bool)
	if !ok {
		a.fail("argument %q must be a boolean, got %T", name, value)
		return fallback
	}
	return b
}

// OptionalInt reads an integer argument. JSON numbers arrive as float64, so
// both representations are accepted.
func (a *Args) OptionalInt(name string, fallback int) int {
	value, ok := a.raw[name]
	if !ok || value == nil {
		return fallback
	}
	switch n := value.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		a.fail("argument %q must be a number, got %T", name, value)
		return fallback
	}
}

func (a *Args) fail(format string, args ...any) {
	a.errs = append(a.errs, fmt.Errorf(format, args...))
}
