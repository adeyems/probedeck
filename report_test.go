package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSavedReportVersionAndNoOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	report := Report{Passed: false, Stopped: "context canceled", Results: []Result{{Name: "synthetic", Skipped: true, Problems: []string{"stopped"}}}}
	if err := saveReport(path, report); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Version int
		Report  Report
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Version != 1 || !saved.Report.Results[0].Skipped || saved.Report.Stopped != "context canceled" {
		t.Fatal(saved)
	}
	if err := saveReport(path, Report{}); err == nil {
		t.Fatal("overwrote output")
	}
	again, _ := os.ReadFile(path)
	if !bytes.Equal(data, again) {
		t.Fatal("changed existing output")
	}
	var out, errs bytes.Buffer
	if code := cli([]string{"run", "missing.json", "--output", path}, &out, &errs); code != 2 {
		t.Fatal(code)
	}
	if errs.String() != "output must be a new file\n" {
		t.Fatal(errs.String())
	}
}

func TestRunSavesSameReportAsStdout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	root := t.TempDir()
	deck := filepath.Join(root, "deck.json")
	saved := filepath.Join(root, "saved.json")
	data, _ := json.Marshal(Deck{Checks: []Check{{Name: "local", URL: server.URL, Status: 200, TimeoutMS: 1000}}})
	if err := os.WriteFile(deck, data, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := cli([]string{"run", deck, "--output", saved}, &out, &errs); code != 0 {
		t.Fatal(code, errs.String())
	}
	raw, _ := os.ReadFile(saved)
	var envelope struct {
		Version int
		Report  json.RawMessage
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	var a, b interface{}
	if err := json.Unmarshal(out.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(envelope.Report, &b); err != nil {
		t.Fatal(err)
	}
	encodedA, _ := json.Marshal(a)
	encodedB, _ := json.Marshal(b)
	if !bytes.Equal(encodedA, encodedB) {
		t.Fatal("saved report differs from stdout")
	}
}
