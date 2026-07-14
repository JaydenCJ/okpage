// Tests for the TOML-subset parser. The parser is the front door for every
// user mistake, so error cases get as much coverage as the happy path.
package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseTOMLScalars(t *testing.T) {
	doc, err := parseTOML(`
name = "okpage"
count = 42
negative = -7
enabled = true
disabled = false
`)
	if err != nil {
		t.Fatal(err)
	}
	want := document{
		"name":     "okpage",
		"count":    int64(42),
		"negative": int64(-7),
		"enabled":  true,
		"disabled": false,
	}
	if !reflect.DeepEqual(doc, want) {
		t.Fatalf("got %#v, want %#v", doc, want)
	}
}

func TestParseTOMLStringEscapes(t *testing.T) {
	doc, err := parseTOML(`s = "a\"b\\c\nd\teé"`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := doc["s"], "a\"b\\c\nd\teé"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestParseTOMLCommentsAndBlankLines(t *testing.T) {
	doc, err := parseTOML(`
# full-line comment
key = "value"   # trailing comment

other = 1
`)
	if err != nil {
		t.Fatal(err)
	}
	if doc["key"] != "value" || doc["other"] != int64(1) {
		t.Fatalf("comments not stripped correctly: %#v", doc)
	}

	// A # inside a quoted string is data, not a comment.
	doc, err = parseTOML(`url = "http://127.0.0.1/#anchor" # real comment`)
	if err != nil {
		t.Fatal(err)
	}
	if got := doc["url"]; got != "http://127.0.0.1/#anchor" {
		t.Fatalf("got %q, want the # preserved", got)
	}
}

func TestParseTOMLArrays(t *testing.T) {
	doc, err := parseTOML(`
strings = ["a", "b, c", "d"]
numbers = [1, 2, 3]
empty = []
`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := doc["strings"], []any{"a", "b, c", "d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("strings: got %#v", got)
	}
	if got, want := doc["numbers"], []any{int64(1), int64(2), int64(3)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("numbers: got %#v", got)
	}
	if got := doc["empty"]; !reflect.DeepEqual(got, []any{}) {
		t.Fatalf("empty: got %#v", got)
	}
}

func TestParseTOMLTables(t *testing.T) {
	doc, err := parseTOML(`
top = "root"

[alerts]
enabled = true
`)
	if err != nil {
		t.Fatal(err)
	}
	table, ok := doc["alerts"].(map[string]any)
	if !ok {
		t.Fatalf("alerts is %#v, want a table", doc["alerts"])
	}
	if table["enabled"] != true {
		t.Fatalf("table key not populated: %#v", table)
	}
	if doc["top"] != "root" {
		t.Fatalf("root key lost: %#v", doc)
	}
}

func TestParseTOMLArrayOfTables(t *testing.T) {
	doc, err := parseTOML(`
[[service]]
name = "a"

[[service]]
name = "b"
`)
	if err != nil {
		t.Fatal(err)
	}
	services, ok := doc["service"].([]map[string]any)
	if !ok || len(services) != 2 {
		t.Fatalf("got %#v, want two service tables", doc["service"])
	}
	if services[0]["name"] != "a" || services[1]["name"] != "b" {
		t.Fatalf("tables filled in the wrong order: %#v", services)
	}
}

// TestParseTOMLRejectsInvalidInput covers every rejection path. Nothing
// outside the documented subset may parse silently — floats truncating to
// ints or duplicate keys shadowing each other would corrupt configs without
// any visible symptom.
func TestParseTOMLRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"unterminated string", "a = 1\nb = \"unterminated", "line 2"},
		{"duplicate key", "a = 1\na = 2", `key "a" is set twice`},
		{"duplicate table", "[t]\na = 1\n[t]\nb = 2", "defined twice"},
		{"float outside subset", "ratio = 1.5", "unsupported value"},
		{"unknown escape", `s = "\x41"`, "unsupported escape"},
		{"malformed header", "[table", "malformed table header"},
		{"invalid key characters", "my key = 1", "invalid key"},
		{"missing equals", "just-a-word", "expected key = value"},
		{"trailing garbage after string", `s = "ok" extra`, "trailing"},
		{"unterminated array", `a = ["x"`, "unterminated array"},
		{"table conflicts with value", "service = 1\n[[service]]\nname = \"x\"", "conflicts"},
		{"trailing comma in array", `a = ["x",]`, "trailing comma"},
	}
	for _, tc := range cases {
		_, err := parseTOML(tc.src)
		if err == nil {
			t.Errorf("%s: expected error containing %q, got nil", tc.name, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not contain %q", tc.name, err, tc.want)
		}
	}
}
