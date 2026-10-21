package training

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestShippedPlanExampleIsValid(t *testing.T) {
	data, err := os.ReadFile("../../docs/examples/plan-upper-lower.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParsePlanDocument(data)
	if err != nil {
		t.Fatalf("example must be valid: %v", err)
	}
	if doc.Name != "Upper A" || len(doc.Items) != 5 {
		t.Fatalf("unexpected document: %+v", doc)
	}
	if doc.Items[0].TargetWeightKG == nil || *doc.Items[0].TargetWeightKG != 70 {
		t.Fatal("target_weight_kg lost")
	}
}

func TestParsePlanDocumentReportsEveryProblemWithPaths(t *testing.T) {
	bad := `{"format":"workout-plan","version":1,"name":"","bogus":1,
	"items":[{"exercise":"a","sets":0},{"exercise":"a","sets":3,"rep_min":9,"rep_max":5,"extra":true},
	{"exercise":"b","sets":3,"target_rir":11}]}`
	_, err := ParsePlanDocument([]byte(bad))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("want validation error, got %v", err)
	}
	got := map[string]bool{}
	for _, i := range IssuesOf(err) {
		got[i.Field] = true
	}
	for _, want := range []string{"name", "bogus", "items[0].sets", "items[1].extra", "items[1].reps", "items[1].exercise", "items[2].target_rir"} {
		if !got[want] {
			t.Errorf("missing issue for %q; got %v", want, IssuesOf(err))
		}
	}
}

func TestParsePlanDocumentRejectsWrongTypesAndSyntax(t *testing.T) {
	for name, in := range map[string]string{
		"syntax":     `{"format":`,
		"not object": `[]`,
		"wrong type": `{"format":"workout-plan","version":1,"name":"x","items":[{"exercise":"a","sets":"three"}]}`,
		"wrong ver":  `{"format":"workout-plan","version":2,"name":"x","items":[{"exercise":"a","sets":3}]}`,
		"wrong fmt":  `{"format":"other","version":1,"name":"x","items":[{"exercise":"a","sets":3}]}`,
	} {
		if _, err := ParsePlanDocument([]byte(in)); !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "validation error") {
			t.Errorf("%s: want validation error, got %v", name, err)
		}
	}
}
