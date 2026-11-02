package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReportComparisonCLI(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.json")
	b := filepath.Join(root, "b.json")
	before := Report{Passed: true, Results: []Result{{Name: "one", Passed: true, Status: 200, Problems: []string{}}, {Name: "two", Passed: true, Status: 200, Problems: []string{}}}}
	after := Report{Passed: true, Results: []Result{before.Results[1], before.Results[0]}}
	if err := saveReport(a, before); err != nil {
		t.Fatal(err)
	}
	if err := saveReport(b, after); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := cli([]string{"compare", a, b}, &out, &errs); code != 0 {
		t.Fatal(code, errs.String())
	}
	after.Results[0].Status = 201
	change := compareReports(before, after)
	if change.Match || len(change.Changes) != 1 || change.Changes[0].Name != "two" {
		t.Fatal(change)
	}
	if err := os.WriteFile(b, []byte(`{"version":99,"report":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := cli([]string{"compare", a, b}, &out, &errs); code != 2 || out.Len() != 0 {
		t.Fatal(code, out.String())
	}
}
func TestInvalidSavedOutcomes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	for _, data := range []string{
		`{"version":1,"report":{"passed":null,"results":[]}}`,
		`{"version":1,"report":{"passed":true,"results":[{"name":"x","passed":true,"problems":["failure"]}]}}`,
		`{"version":1,"report":{"passed":true,"results":[{"name":"x","passed":true,"problems":null}]}}`,
		`{"version":1,"version":1,"report":{}}`,
	} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readSavedReport(path); err == nil {
			t.Fatal("accepted", data)
		}
	}
}
