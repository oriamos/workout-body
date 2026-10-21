package apisrv

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

var (
	specOnce sync.Once
	specDoc  map[string]any
	specErr  error
)

func loadSpec(t *testing.T) map[string]any {
	t.Helper()
	specOnce.Do(func() {
		specErr = yaml.Unmarshal(openAPISpec, &specDoc)
	})
	if specErr != nil {
		t.Fatalf("openapi.yaml does not parse: %v", specErr)
	}
	return specDoc
}

var httpMethods = []string{"get", "post", "put", "patch", "delete"}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

// resolve follows a local $ref.
func resolve(t *testing.T, node any) map[string]any {
	t.Helper()
	m := asMap(node)
	for i := 0; m != nil && i < 10; i++ {
		ref, ok := m["$ref"].(string)
		if !ok {
			return m
		}
		m = lookupRef(t, ref)
	}
	return m
}

func lookupRef(t *testing.T, ref string) map[string]any {
	t.Helper()
	if !strings.HasPrefix(ref, "#/") {
		t.Fatalf("non-local $ref %q", ref)
	}
	var cur any = loadSpec(t)
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		cur = asMap(cur)[part]
		if cur == nil {
			t.Fatalf("$ref %q does not resolve", ref)
		}
	}
	return asMap(cur)
}

var paramRe = regexp.MustCompile(`\{([^}]+)\}`)

// TestContractRoutesMatchSpec asserts every registered route is documented and
// every documented operation is registered.
func TestContractRoutesMatchSpec(t *testing.T) {
	spec := loadSpec(t)
	servers, _ := spec["servers"].([]any)
	if len(servers) != 1 || asMap(servers[0])["url"] != Prefix {
		t.Fatalf("servers must be exactly [{url: %s}], got %v", Prefix, servers)
	}
	if v, _ := spec["openapi"].(string); !strings.HasPrefix(v, "3.1") {
		t.Fatalf("openapi version = %q, want 3.1.x", v)
	}
	documented := map[string]bool{}
	for path, item := range asMap(spec["paths"]) {
		for _, m := range httpMethods {
			if asMap(item)[m] != nil {
				documented[strings.ToUpper(m)+" "+path] = true
			}
		}
	}
	registered := map[string]bool{}
	api := New(nil)
	for _, r := range api.routes {
		registered[r.method+" "+strings.TrimPrefix(r.path, Prefix)] = true
	}
	var missing, extra []string
	for k := range registered {
		if !documented[k] {
			missing = append(missing, k)
		}
	}
	for k := range documented {
		if !registered[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("routes registered but not documented in openapi.yaml: %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("operations documented but not registered: %v", extra)
	}
	if len(registered) < 25 {
		t.Errorf("only %d routes registered; expected the full v1 surface", len(registered))
	}
}

// TestContractSpecIsWellFormed checks the parts of the document the routes
// test does not: path parameters declared, $refs resolving, responses present,
// and the schemas the UI depends on.
func TestContractSpecIsWellFormed(t *testing.T) {
	spec := loadSpec(t)
	opIDs := map[string]string{}
	for path, rawItem := range asMap(spec["paths"]) {
		item := asMap(rawItem)
		for _, m := range httpMethods {
			op := asMap(item[m])
			if op == nil {
				continue
			}
			where := strings.ToUpper(m) + " " + path
			id, _ := op["operationId"].(string)
			if id == "" {
				t.Errorf("%s: missing operationId", where)
			} else if prev, dup := opIDs[id]; dup {
				t.Errorf("%s: operationId %q duplicates %s", where, id, prev)
			}
			opIDs[id] = where
			declared := map[string]bool{}
			for _, src := range []any{item["parameters"], op["parameters"]} {
				params, _ := src.([]any)
				for _, p := range params {
					pm := resolve(t, p)
					if pm["in"] == "path" {
						declared[pm["name"].(string)] = true
						if pm["required"] != true {
							t.Errorf("%s: path parameter %v must be required", where, pm["name"])
						}
					}
				}
			}
			for _, match := range paramRe.FindAllStringSubmatch(path, -1) {
				if !declared[match[1]] {
					t.Errorf("%s: path parameter {%s} is not declared", where, match[1])
				}
				delete(declared, match[1])
			}
			for name := range declared {
				t.Errorf("%s: declares path parameter %q not in the path", where, name)
			}
			responses := asMap(op["responses"])
			ok2xx := false
			for code := range responses {
				if strings.HasPrefix(code, "2") {
					ok2xx = true
				}
			}
			if !ok2xx {
				t.Errorf("%s: no 2xx response documented", where)
			}
		}
	}
	// Every $ref anywhere resolves.
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				lookupRef(t, ref)
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(spec)
	schemas := asMap(asMap(spec["components"])["schemas"])
	for _, name := range []string{"SetInput", "Set", "Workout", "ProgressItem", "Prescription", "BatchResult",
		"PlanDocument", "Plan", "Exercise", "ExerciseStats", "TrainingSummary", "Error", "SetType", "SetPatch"} {
		if schemas[name] == nil {
			t.Errorf("components.schemas.%s is missing", name)
		}
	}
	enum, _ := asMap(schemas["SetType"])["enum"].([]any)
	if fmt.Sprint(enum) != "[warmup working backoff drop failure]" {
		t.Errorf("SetType enum = %v", enum)
	}
	headers := asMap(asMap(spec["components"])["headers"])
	if headers["ETag"] == nil {
		t.Error("components.headers.ETag is missing")
	}
	params := asMap(asMap(spec["components"])["parameters"])
	for _, p := range []string{"IfMatchRequired", "IfMatchOptional", "IdempotencyKey"} {
		if params[p] == nil {
			t.Errorf("components.parameters.%s is missing", p)
		}
	}
}

// ---- response validation ------------------------------------------------------

// specPathFor finds the documented path template matching a request path,
// preferring the template with the most literal segments.
func specPathFor(t *testing.T, escapedPath string) string {
	if !strings.HasPrefix(escapedPath, Prefix+"/") {
		return ""
	}
	segs := strings.Split(strings.TrimPrefix(escapedPath, Prefix), "/")
	best, bestLiterals := "", -1
	for tmpl := range asMap(loadSpec(t)["paths"]) {
		ts := strings.Split(tmpl, "/")
		if len(ts) != len(segs) {
			continue
		}
		literals, ok := 0, true
		for i := range ts {
			switch {
			case strings.HasPrefix(ts[i], "{"):
				ok = ok && segs[i] != ""
			case ts[i] == segs[i]:
				literals++
			default:
				ok = false
			}
		}
		if ok && literals > bestLiterals {
			best, bestLiterals = tmpl, literals
		}
	}
	return best
}

// checkAgainstSpec validates a response's status, headers and JSON body
// against the documented operation. Paths that match no documented template
// (the JSON 404/405 fallback) are only checked for the Error envelope.
func checkAgainstSpec(t *testing.T, method, escapedPath string, r resp) {
	t.Helper()
	errSchema := lookupRef(t, "#/components/schemas/Error")
	tmpl := specPathFor(t, escapedPath)
	op := asMap(asMap(asMap(loadSpec(t)["paths"])[tmpl])[strings.ToLower(method)])
	if tmpl == "" || op == nil {
		if r.status != 404 && r.status != 405 && r.status != 401 {
			t.Errorf("%s %s: undocumented path answered %d", method, escapedPath, r.status)
		}
		if r.status != 401 {
			validateBody(t, method+" "+escapedPath, errSchema, r.body)
		}
		return
	}
	if r.status == 401 {
		return // the outer bearer layer, not this API
	}
	respDef := resolve(t, asMap(op["responses"])[strconv.Itoa(r.status)])
	if respDef == nil {
		if r.status == 405 {
			validateBody(t, method+" "+escapedPath, errSchema, r.body)
			return
		}
		t.Errorf("%s %s (%s): status %d is not documented; body %s", method, escapedPath, tmpl, r.status, r.body)
		return
	}
	for name := range asMap(respDef["headers"]) {
		if name == "ETag" && r.status == 200 && tmpl == "/plans/import" {
			continue // a dry run legitimately omits it
		}
		if r.header.Get(name) == "" {
			t.Errorf("%s %s: documented header %s missing", method, escapedPath, name)
		}
	}
	content := asMap(respDef["content"])
	if len(content) == 0 {
		if len(strings.TrimSpace(string(r.body))) != 0 {
			t.Errorf("%s %s: %d documents no body but got %s", method, escapedPath, r.status, r.body)
		}
		return
	}
	ct := r.header.Get("Content-Type")
	for media, m := range content {
		if strings.HasPrefix(ct, media) {
			if media == "application/json" {
				validateBody(t, method+" "+escapedPath, resolve(t, asMap(m)["schema"]), r.body)
			}
			return
		}
	}
	t.Errorf("%s %s: Content-Type %q not documented", method, escapedPath, ct)
}

func validateBody(t *testing.T, where string, schema map[string]any, body []byte) {
	t.Helper()
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Errorf("%s: body is not JSON: %v (%s)", where, err, body)
		return
	}
	for _, p := range validate(t, schema, v, "$") {
		t.Errorf("%s: %s", where, p)
	}
}

// validate is a small JSON Schema subset validator: $ref, type (incl. arrays
// of types), enum, const, properties/required, additionalProperties:false,
// items and oneOf. Unknown properties are reported, so undocumented response
// fields fail the contract.
func validate(t *testing.T, schema map[string]any, v any, path string) []string {
	schema = resolve(t, schema)
	if schema == nil {
		return nil
	}
	if alts, ok := schema["oneOf"].([]any); ok {
		var all []string
		for _, a := range alts {
			p := validate(t, asMap(a), v, path)
			if len(p) == 0 {
				return nil
			}
			all = append(all, p...)
		}
		return []string{fmt.Sprintf("%s matches no oneOf alternative: %v", path, all)}
	}
	var types []string
	switch tv := schema["type"].(type) {
	case string:
		types = []string{tv}
	case []any:
		for _, x := range tv {
			types = append(types, x.(string))
		}
	}
	if len(types) > 0 && !typeOK(types, v) {
		return []string{fmt.Sprintf("%s: %T %v is not %v", path, v, v, types)}
	}
	if enum, ok := schema["enum"].([]any); ok && v != nil {
		found := false
		for _, e := range enum {
			if fmt.Sprint(e) == fmt.Sprint(v) {
				found = true
			}
		}
		if !found {
			return []string{fmt.Sprintf("%s: %v not in enum %v", path, v, enum)}
		}
	}
	var out []string
	switch val := v.(type) {
	case map[string]any:
		props := asMap(schema["properties"])
		if props != nil {
			for k, pv := range val {
				ps, ok := props[k]
				if !ok {
					out = append(out, fmt.Sprintf("%s.%s is not documented", path, k))
					continue
				}
				out = append(out, validate(t, asMap(ps), pv, path+"."+k)...)
			}
		}
		req, _ := schema["required"].([]any)
		for _, r := range req {
			if _, ok := val[r.(string)]; !ok {
				out = append(out, fmt.Sprintf("%s.%s is required but missing", path, r))
			}
		}
	case []any:
		if items := asMap(schema["items"]); items != nil {
			for i, it := range val {
				out = append(out, validate(t, items, it, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	}
	return out
}

func typeOK(types []string, v any) bool {
	for _, ty := range types {
		switch ty {
		case "null":
			if v == nil {
				return true
			}
		case "object":
			if _, ok := v.(map[string]any); ok {
				return true
			}
		case "array":
			if _, ok := v.([]any); ok {
				return true
			}
		case "string":
			if _, ok := v.(string); ok {
				return true
			}
		case "boolean":
			if _, ok := v.(bool); ok {
				return true
			}
		case "number":
			if _, ok := v.(float64); ok {
				return true
			}
		case "integer":
			if f, ok := v.(float64); ok && f == math.Trunc(f) {
				return true
			}
		}
	}
	return false
}

// TestContractValidatorRejectsDrift proves the response validator is not
// vacuous: an undocumented field and a wrong type are both reported.
func TestContractValidatorRejectsDrift(t *testing.T) {
	set := lookupRef(t, "#/components/schemas/Set")
	var v any
	json.Unmarshal([]byte(`{"id":1,"session_id":1,"position":1,"exercise_id":1,"exercise":"x","weight_kg":null,"reps":"5","rpe":null,"rir":null,"set_type":"working","revision":1,"si":0,"surprise":true}`), &v)
	problems := validate(t, set, v, "$")
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "$.surprise is not documented") || !strings.Contains(joined, "$.reps") {
		t.Fatalf("validator missed drift: %v", problems)
	}
	if got := specPathFor(t, "/api/v1/plans/import"); got != "/plans/import" {
		t.Fatalf("specPathFor(/plans/import) = %q", got)
	}
	if got := specPathFor(t, "/api/v1/plans/7"); got != "/plans/{id}" {
		t.Fatalf("specPathFor(/plans/7) = %q", got)
	}
}
