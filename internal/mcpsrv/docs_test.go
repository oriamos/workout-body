package mcpsrv

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var updateDocs = flag.Bool("update-docs", false, "rewrite docs/mcp-tools.md from the registered tools")

// TestToolReferenceIsUpToDate renders docs/mcp-tools.md from the tools the
// server actually registers, so the reference cannot drift from the code.
// Regenerate with: go test ./internal/mcpsrv -run ToolReference -update-docs
func TestToolReferenceIsUpToDate(t *testing.T) {
	cs := connectInMemory(t, New(newService(t, "")))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := res.Tools
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })

	var b strings.Builder
	b.WriteString("# MCP tool reference\n\n")
	b.WriteString("<!-- GENERATED from the registered tools by internal/mcpsrv/docs_test.go. Do not edit by hand; run:\n")
	b.WriteString("     go test ./internal/mcpsrv -run ToolReference -update-docs -->\n\n")
	b.WriteString("This server registers " + fmt.Sprint(len(tools)) + " tools. Descriptions below are exactly what the model receives.\n\n")
	b.WriteString("Worked examples with real calls and results: [mcp-examples.md](mcp-examples.md). Connection and plugin setup: [plugin-setup.md](plugin-setup.md).\n\n")
	b.WriteString("Conventions that apply to every tool:\n\n")
	b.WriteString("* Units: weight in **kilograms**, RPE 1–10, RIR 0–10.\n")
	b.WriteString("* Dates are local calendar dates `YYYY-MM-DD` in the configured timezone (`WORKOUT_TIMEZONE`, default `Asia/Jerusalem`). Omitting a date means today there.\n")
	b.WriteString("* Omitted/`null` metrics mean *not recorded*. Nothing is ever defaulted or guessed.\n")
	b.WriteString("* `expected_revision` makes a write fail with a conflict if the record changed since you read it.\n")
	b.WriteString("* `idempotency_key` makes a retried write safe: same key and payload replays the first result; same key with a different payload is a conflict.\n")
	b.WriteString("* Validation errors list every problem with its field path, e.g. `sets[1].reps: …`.\n\n")

	b.WriteString("| Tool | Kind |\n|---|---|\n")
	for _, tl := range tools {
		fmt.Fprintf(&b, "| [`%s`](#%s) | %s |\n", tl.Name, tl.Name, kind(tl.Annotations))
	}
	b.WriteString("\n")
	for _, tl := range tools {
		fmt.Fprintf(&b, "## %s\n\n", tl.Name)
		fmt.Fprintf(&b, "**%s** — %s\n\n", tl.Annotations.Title, kind(tl.Annotations))
		b.WriteString(strings.TrimSpace(tl.Description) + "\n\n")
		if rows := paramRows(tl.InputSchema); len(rows) > 0 {
			b.WriteString("| Parameter | Type | Required | Description |\n|---|---|---|---|\n")
			for _, r := range rows {
				b.WriteString(r + "\n")
			}
			b.WriteString("\n")
		} else {
			b.WriteString("_No parameters._\n\n")
		}
	}
	got := b.String()
	const path = "../../docs/mcp-tools.md"
	if *updateDocs {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (generate it with -update-docs)", err)
	}
	if string(want) != got {
		t.Fatal("docs/mcp-tools.md is stale; regenerate with: go test ./internal/mcpsrv -run ToolReference -update-docs")
	}
}

func kind(a *mcp.ToolAnnotations) string {
	switch {
	case a == nil:
		return "unspecified"
	case a.ReadOnlyHint:
		return "read-only"
	case a.DestructiveHint != nil && *a.DestructiveHint:
		return "destructive write"
	case a.IdempotentHint:
		return "write (idempotent)"
	default:
		return "write"
	}
}

// paramRows flattens the top level of a JSON Schema object into table rows.
func paramRows(schema any) []string {
	m, _ := schema.(map[string]any)
	props, _ := m["properties"].(map[string]any)
	required := map[string]bool{}
	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if name, ok := r.(string); ok {
				required[name] = true
			}
		}
	}
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	sort.Strings(names)
	var rows []string
	for _, n := range names {
		p, _ := props[n].(map[string]any)
		req := "no"
		if required[n] {
			req = "yes"
		}
		desc, _ := p["description"].(string)
		rows = append(rows, fmt.Sprintf("| `%s` | %s | %s | %s |", n, schemaType(p), req, cell(desc)))
	}
	return rows
}

func schemaType(p map[string]any) string {
	switch t := p["type"].(type) {
	case string:
		if t == "array" {
			if items, ok := p["items"].(map[string]any); ok {
				return "array of " + schemaType(items)
			}
		}
		return t
	case []any:
		var parts []string
		for _, v := range t {
			if s, ok := v.(string); ok && s != "null" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "|")
	}
	return "any"
}

func cell(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	return strings.ReplaceAll(s, "|", "\\|")
}
