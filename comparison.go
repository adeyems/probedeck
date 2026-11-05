package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
)

func readSavedReport(path string) (Report, error) {
	var report Report
	f, err := os.Open(path)
	if err != nil {
		return report, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 8*maxBytes+1))
	if err != nil {
		return report, err
	}
	if len(data) > 8*maxBytes {
		return report, fmt.Errorf("saved report exceeds 8 MiB")
	}
	if err := uniqueKeys(data); err != nil {
		return report, err
	}
	var envelope struct {
		Version int    `json:"version"`
		Report  Report `json:"report"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return report, err
	}
	if envelope.Version != 1 {
		return report, fmt.Errorf("unsupported report version")
	}
	// Verify required booleans and arrays: decoding null to zero values must not hide invalid data.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return report, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw["report"], &body); err != nil {
		return report, err
	}
	validBool := func(raw json.RawMessage) bool {
		return string(bytes.TrimSpace(raw)) == "true" || string(bytes.TrimSpace(raw)) == "false"
	}
	if !validBool(body["passed"]) || len(envelope.Report.Results) < 1 || len(envelope.Report.Results) > 100 {
		return report, fmt.Errorf("invalid saved report summary")
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(body["results"], &rows); err != nil {
		return report, err
	}
	passed := true
	names := map[string]bool{}
	for index, result := range envelope.Report.Results {
		fields := rows[index]
		if strings.TrimSpace(result.Name) == "" || names[result.Name] || !validBool(fields["passed"]) || result.Problems == nil {
			return report, fmt.Errorf("invalid saved check result")
		}
		var problemValues []interface{}
		if err := json.Unmarshal(fields["problems"], &problemValues); err != nil {
			return report, err
		}
		for _, value := range problemValues {
			if _, ok := value.(string); !ok {
				return report, fmt.Errorf("invalid problem text")
			}
		}
		names[result.Name] = true
		if value, ok := fields["skipped"]; ok && !validBool(value) {
			return report, fmt.Errorf("invalid skipped flag")
		}
		if value, ok := fields["status"]; ok && (string(value) == "null" || result.Status < 100 || result.Status > 599) {
			return report, fmt.Errorf("invalid HTTP status")
		}
		if result.Passed != (len(result.Problems) == 0 && !result.Skipped) {
			return report, fmt.Errorf("inconsistent check outcome")
		}
		passed = passed && result.Passed
	}
	if value, ok := body["stopped"]; ok && string(value) == "null" {
		return report, fmt.Errorf("invalid stopped reason")
	}
	if envelope.Report.Passed != (passed && envelope.Report.Stopped == "") {
		return report, fmt.Errorf("inconsistent report outcome")
	}
	return envelope.Report, nil
}

type ReportChange struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}
type ReportComparison struct {
	Version        int            `json:"version"`
	Match          bool           `json:"match"`
	StoppedChanged bool           `json:"stopped_changed"`
	Changes        []ReportChange `json:"changes"`
}

func compareReports(before, after Report) ReportComparison {
	old, newResults := map[string]Result{}, map[string]Result{}
	names := map[string]bool{}
	for _, r := range before.Results {
		old[r.Name] = r
		names[r.Name] = true
	}
	for _, r := range after.Results {
		newResults[r.Name] = r
		names[r.Name] = true
	}
	ordered := []string{}
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	report := ReportComparison{Version: 1, Changes: []ReportChange{}, StoppedChanged: before.Stopped != after.Stopped}
	for _, name := range ordered {
		a, aok := old[name]
		b, bok := newResults[name]
		kind := ""
		if !aok {
			kind = "added"
		} else if !bok {
			kind = "removed"
		} else if !reflect.DeepEqual(a, b) {
			kind = "changed"
		}
		if kind != "" {
			report.Changes = append(report.Changes, ReportChange{name, kind})
		}
	}
	report.Match = len(report.Changes) == 0 && !report.StoppedChanged
	return report
}
func comparisonCLI(args []string, out, errs io.Writer) int {
	textOutput := len(args) == 4 && args[3] == "--text"
	if len(args) != 3 && !textOutput {
		fmt.Fprintln(errs, "usage: probedeck compare BEFORE.json AFTER.json [--text]")
		return 2
	}
	a, err := readSavedReport(args[1])
	if err != nil {
		fmt.Fprintln(errs, "invalid baseline report:", err)
		return 2
	}
	b, err := readSavedReport(args[2])
	if err != nil {
		fmt.Fprintln(errs, "invalid candidate report:", err)
		return 2
	}
	result := compareReports(a, b)
	if textOutput {
		_, err = io.WriteString(out, comparisonText(result))
	} else {
		err = json.NewEncoder(out).Encode(result)
	}
	if err != nil {
		fmt.Fprintln(errs, "cannot write comparison")
		return 2
	}
	if !result.Match {
		return 1
	}
	return 0
}

// Quote names so fixture-controlled newlines and terminal escapes remain visible text.
func comparisonText(result ReportComparison) string {
	var text strings.Builder
	if result.Match {
		return "Reports match.\n"
	}
	fmt.Fprintf(&text, "Reports differ: %d check change(s).\n", len(result.Changes))
	if result.StoppedChanged {
		text.WriteString("Run stop reason changed.\n")
	}
	for _, change := range result.Changes {
		fmt.Fprintf(&text, "%s: %q\n", change.Kind, change.Name)
	}
	return text.String()
}
