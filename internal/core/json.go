package core

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// Minimal typed JSON accessors.
//
// The C# version hand-rolled a parser because .NET Framework 3.5 had no
// System.Text.Json. Go's encoding/json is stdlib, so this file only supplies
// the JObj/JArr convenience layer the adapters are written against — with the
// same "safe default on missing or mismatched type" contract.
//
// Numbers are decoded with UseNumber so large token counts survive without
// going through float64 (the C# parser lost precision above 2^53).

// ParseJSON decodes text into any of: map[string]any, []any, string,
// json.Number, bool, nil. It returns nil on any failure, mirroring
// Json.Parse's swallow-errors contract — callers treat nil as "unreadable".
func ParseJSON(text string) any {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil
	}
	return v
}

// JObj is a typed accessor over a JSON object. The zero value is a valid
// empty object, so no nil checks are needed anywhere.
type JObj struct{ m map[string]any }

// Of coerces a decoded value to JObj, returning the empty object when the
// value is not an object.
func Of(raw any) JObj {
	if m, ok := raw.(map[string]any); ok {
		return JObj{m: m}
	}
	return JObj{}
}

// Has reports whether the key is present (even with a null value).
func (o JObj) Has(key string) bool {
	_, ok := o.m[key]
	return ok
}

// Keys returns the top-level key names. Order is unspecified.
func (o JObj) Keys() []string {
	out := make([]string, 0, len(o.m))
	for k := range o.m {
		out = append(out, k)
	}
	return out
}

// Len returns the number of keys.
func (o JObj) Len() int { return len(o.m) }

// Raw returns the undecoded value, or nil when absent.
func (o JObj) Raw(key string) any { return o.m[key] }

// Str returns the string value, or "" when absent or of another type.
func (o JObj) Str(key string) string {
	s, _ := o.m[key].(string)
	return s
}

// StrOk returns the string value and whether it was present as a string.
func (o JObj) StrOk(key string) (string, bool) {
	s, ok := o.m[key].(string)
	return s, ok
}

// StrOr returns the string value, or fallback when absent.
func (o JObj) StrOr(key, fallback string) string {
	if s, ok := o.m[key].(string); ok {
		return s
	}
	return fallback
}

// Obj returns the nested object, or the empty object when absent.
func (o JObj) Obj(key string) JObj { return Of(o.m[key]) }

// Arr returns the nested array, or an empty array when absent.
func (o JObj) Arr(key string) JArr { return ArrOf(o.m[key]) }

// Bool returns the boolean value and whether it was present as a bool.
func (o JObj) Bool(key string) (bool, bool) {
	b, ok := o.m[key].(bool)
	return b, ok
}

// BoolOr returns the boolean value, or fallback when absent or mistyped.
func (o JObj) BoolOr(key string, fallback bool) bool {
	if b, ok := o.m[key].(bool); ok {
		return b
	}
	return fallback
}

// Num returns the numeric value and whether it was readable as a number.
// A numeric string is accepted, matching the C# accessor.
func (o JObj) Num(key string) (float64, bool) { return asFloat(o.m[key]) }

// Long returns the numeric value truncated to int64. Values outside int64
// range report false, matching the C# guard.
func (o JObj) Long(key string) (int64, bool) {
	f, ok := asFloat(o.m[key])
	if !ok || f >= 9.22e18 || f <= -9.22e18 {
		return 0, false
	}
	return int64(f), true
}

// Int returns the numeric value clamped to int, and whether it was readable.
func (o JObj) Int(key string) (int, bool) {
	l, ok := o.Long(key)
	if !ok {
		return 0, false
	}
	if l > int64(maxInt) {
		return maxInt, true
	}
	if l < int64(minInt) {
		return minInt, true
	}
	return int(l), true
}

const (
	maxInt = int(^uint(0) >> 1)
	minInt = -maxInt - 1
)

// JArr is a typed accessor over a JSON array. The zero value is a valid
// empty array.
type JArr struct{ a []any }

// ArrOf coerces a decoded value to JArr, returning the empty array when the
// value is not an array.
func ArrOf(raw any) JArr {
	if a, ok := raw.([]any); ok {
		return JArr{a: a}
	}
	return JArr{}
}

// Len returns the element count.
func (a JArr) Len() int { return len(a.a) }

// At returns the element, or nil when out of range.
func (a JArr) At(i int) any {
	if i >= 0 && i < len(a.a) {
		return a.a[i]
	}
	return nil
}

// ObjAt returns the element as an object, or the empty object.
func (a JArr) ObjAt(i int) JObj { return Of(a.At(i)) }

// StrAt returns the element as a string, or "".
func (a JArr) StrAt(i int) string {
	s, _ := a.At(i).(string)
	return s
}

// NumAt returns the element as a number, and whether it was numeric.
func (a JArr) NumAt(i int) (float64, bool) { return asFloat(a.At(i)) }

// Items returns the raw elements for range loops.
func (a JArr) Items() []any { return a.a }

// Objects returns only the object-typed elements.
func (a JArr) Objects() []JObj {
	out := make([]JObj, 0, len(a.a))
	for _, v := range a.a {
		if _, ok := v.(map[string]any); ok {
			out = append(out, Of(v))
		}
	}
	return out
}

// asFloat reads a decoded value as a float64, accepting json.Number and
// numeric strings.
func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f, true
		}
		return 0, false
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
			return f, true
		}
		return 0, false
	default:
		return 0, false
	}
}
