package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxBytes = 1024 * 1024

type JSONAssertion struct {
	Pointer string          `json:"pointer"`
	Equals  json.RawMessage `json:"equals,omitempty"`
	Type    string          `json:"type,omitempty"`
}

type Check struct {
	Method    string            `json:"method,omitempty"`
	Body      json.RawMessage   `json:"body,omitempty"`
	JSON      []JSONAssertion   `json:"json,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Name      string            `json:"name"`
	URL       string            `json:"url"`
	Status    int               `json:"status"`
	Contains  string            `json:"contains,omitempty"`
	TimeoutMS int               `json:"timeout_ms"`
}
type Deck struct {
	Checks []Check `json:"checks"`
}
type Result struct {
	Skipped  bool     `json:"skipped,omitempty"`
	Name     string   `json:"name"`
	Passed   bool     `json:"passed"`
	Status   int      `json:"status,omitempty"`
	Problems []string `json:"problems"`
}
type Report struct {
	Stopped string   `json:"stopped,omitempty"`
	Passed  bool     `json:"passed"`
	Results []Result `json:"results"`
}

// Walk tokens before struct decoding so duplicate names cannot silently override policy.
func uniqueKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 128 {
			return fmt.Errorf("deck nesting exceeds 128 levels")
		}
		token, err := dec.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				token, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok {
					return fmt.Errorf("expected object key")
				}
				if seen[key] {
					return fmt.Errorf("duplicate JSON object key")
				}
				seen[key] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for dec.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter")
		}
		_, err = dec.Token()
		return err
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("expected one JSON document")
	}
	return nil
}

func readDeck(r io.Reader) (Deck, error) {
	var d Deck
	b, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return d, err
	}
	if len(b) > maxBytes {
		return d, fmt.Errorf("deck exceeds 1 MiB")
	}
	if err := uniqueKeys(b); err != nil {
		return d, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&d); err != nil {
		return d, err
	}
	var extra interface{}
	if err = dec.Decode(&extra); err != io.EOF {
		return d, fmt.Errorf("expected one JSON document")
	}
	if len(d.Checks) == 0 || len(d.Checks) > 100 {
		return d, fmt.Errorf("deck must have 1 to 100 checks")
	}
	names := map[string]bool{}
	for _, c := range d.Checks {
		if strings.TrimSpace(c.Name) == "" || names[c.Name] {
			return d, fmt.Errorf("check names must be nonempty and unique")
		}
		if _, err := requestMethod(c); err != nil {
			return d, fmt.Errorf("%s: %w", c.Name, err)
		}
		for _, a := range c.JSON {
			if _, err := pointerParts(a.Pointer); err != nil || (len(a.Equals) == 0) == (a.Type == "") || (a.Type != "" && !validJSONType(a.Type)) {
				return d, fmt.Errorf("%s: JSON assertion requires a valid pointer and exactly one of equals or type (object, array, string, number, boolean, null)", c.Name)
			}
		}
		for name := range c.Headers {
			if !validHeaderName(name) {
				return d, fmt.Errorf("%s: invalid assertion header name", c.Name)
			}
		}
		names[c.Name] = true
		u, e := url.Parse(c.URL)
		if e != nil || u.Scheme != "http" || u.User != nil || u.Fragment != "" {
			return d, fmt.Errorf("%s: use an HTTP loopback URL without credentials or fragment", c.Name)
		}
		ip := net.ParseIP(u.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return d, fmt.Errorf("%s: URL host must be a literal loopback IP", c.Name)
		}
		if c.Status < 100 || c.Status > 599 {
			return d, fmt.Errorf("%s: status must be 100 to 599", c.Name)
		}
		if c.TimeoutMS < 1 || c.TimeoutMS > 30000 {
			return d, fmt.Errorf("%s: timeout_ms must be 1 to 30000", c.Name)
		}
	}
	return d, nil
}

func validHeaderName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return true
}

// JSON Pointer uses exact object keys and canonical zero-based array indices.
func pointerParts(p string) ([]string, error) {
	if p == "" {
		return nil, nil
	}
	if !strings.HasPrefix(p, "/") {
		return nil, fmt.Errorf("pointer must start with slash")
	}
	parts := strings.Split(p[1:], "/")
	for i, part := range parts {
		for j := 0; j < len(part); j++ {
			if part[j] == '~' {
				j++
				if j == len(part) || (part[j] != '0' && part[j] != '1') {
					return nil, fmt.Errorf("invalid pointer escape")
				}
			}
		}
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return parts, nil
}
func jsonValue(b []byte) (interface{}, error) {
	var value interface{}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var extra interface{}
	if dec.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("expected one JSON value")
	}
	return value, nil
}
func validJSONType(kind string) bool {
	switch kind {
	case "object", "array", "string", "number", "boolean", "null":
		return true
	}
	return false
}
func matchesJSONType(value interface{}, kind string) bool {
	switch value.(type) {
	case nil:
		return kind == "null"
	case map[string]interface{}:
		return kind == "object"
	case []interface{}:
		return kind == "array"
	case string:
		return kind == "string"
	case json.Number:
		return kind == "number"
	case bool:
		return kind == "boolean"
	}
	return false
}

func assertStructured(c Check, headers http.Header, body []byte) []string {
	problems := []string{}
	names := []string{}
	for name := range c.Headers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		values := headers.Values(name)
		if len(values) == 0 {
			problems = append(problems, "header missing: "+name)
		} else if len(values) != 1 || values[0] != c.Headers[name] {
			problems = append(problems, "header value mismatch: "+name)
		}
	}
	if len(c.JSON) == 0 {
		return problems
	}
	root, err := jsonValue(body)
	if err != nil {
		return append(problems, "response is not one valid JSON value")
	}
	for _, a := range c.JSON {
		parts, err := pointerParts(a.Pointer)
		value, found := root, err == nil
		for _, part := range parts {
			switch current := value.(type) {
			case map[string]interface{}:
				value, found = current[part]
			case []interface{}:
				index, e := strconv.Atoi(part)
				found = e == nil && index >= 0 && index < len(current) && strconv.Itoa(index) == part
				if found {
					value = current[index]
				}
			default:
				found = false
			}
			if !found {
				break
			}
		}
		expected, e := jsonValue(a.Equals)
		if !found {
			problems = append(problems, "JSON pointer missing: "+a.Pointer)
		} else if a.Type != "" {
			if !matchesJSONType(value, a.Type) {
				problems = append(problems, "JSON type mismatch: "+a.Pointer+" (expected "+a.Type+")")
			}
		} else if e != nil || !reflect.DeepEqual(value, expected) {
			problems = append(problems, "JSON value mismatch: "+a.Pointer)
		}
	}
	return problems
}

func requestMethod(c Check) (string, error) {
	method := c.Method
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
	default:
		return "", fmt.Errorf("unsupported HTTP method")
	}
	if len(c.Body) > 0 {
		if method == http.MethodGet || method == http.MethodHead {
			return "", fmt.Errorf("GET and HEAD cannot have a JSON body")
		}
		if !json.Valid(c.Body) {
			return "", fmt.Errorf("body must be valid JSON")
		}
	}
	return method, nil
}

func runDeck(ctx context.Context, d Deck) Report {
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	report := Report{Passed: true, Results: []Result{}}
	for _, c := range d.Checks {
		if ctx.Err() != nil {
			report.Passed = false
			report.Stopped = ctx.Err().Error()
			report.Results = append(report.Results, Result{Name: c.Name, Skipped: true, Problems: []string{"run stopped before check"}})
			continue
		}
		result := Result{Name: c.Name, Problems: []string{}}
		func() {
			checkCtx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutMS)*time.Millisecond)
			defer cancel()
			method, err := requestMethod(c)
			if err != nil {
				result.Problems = append(result.Problems, "invalid request method or body")
				return
			}
			req, err := http.NewRequestWithContext(checkCtx, method, c.URL, bytes.NewReader(c.Body))
			if err != nil {
				result.Problems = append(result.Problems, "invalid request")
				return
			}
			if len(c.Body) > 0 {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := client.Do(req)
			if err != nil {
				result.Problems = append(result.Problems, "request failed or timed out")
				return
			}
			defer resp.Body.Close()
			result.Status = resp.StatusCode
			if resp.StatusCode != c.Status {
				result.Problems = append(result.Problems, fmt.Sprintf("expected status %d, got %d", c.Status, resp.StatusCode))
			}
			b, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
			if err != nil {
				result.Problems = append(result.Problems, "response read failed")
				return
			}
			if len(b) > maxBytes {
				result.Problems = append(result.Problems, "response exceeds 1 MiB")
				return
			}
			result.Problems = append(result.Problems, assertStructured(c, resp.Header, b)...)
			if c.Contains != "" && !bytes.Contains(b, []byte(c.Contains)) {
				result.Problems = append(result.Problems, "required text missing")
			}
		}()
		if ctx.Err() != nil {
			report.Stopped = ctx.Err().Error()
			result.Problems = append(result.Problems, "run stopped: "+report.Stopped)
		}
		result.Passed = len(result.Problems) == 0
		if !result.Passed {
			report.Passed = false
		}
		report.Results = append(report.Results, result)
	}
	return report
}

// Independent checks only: each worker owns its HTTP lifecycle; results retain deck order.
func runConcurrent(ctx context.Context, d Deck, workers int) Report {
	if workers < 1 || workers > 32 {
		return Report{Passed: false, Stopped: "invalid concurrency", Results: []Result{}}
	}
	if workers == 1 {
		return runDeck(ctx, d)
	}
	results := make([]Report, len(d.Checks))
	jobs := make(chan int)
	var group sync.WaitGroup
	if workers > len(d.Checks) {
		workers = len(d.Checks)
	}
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				results[index] = runDeck(ctx, Deck{Checks: []Check{d.Checks[index]}})
			}
		}()
	}
	for index := range d.Checks {
		jobs <- index
	}
	close(jobs)
	group.Wait()
	report := Report{Passed: true, Results: []Result{}}
	for _, item := range results {
		report.Results = append(report.Results, item.Results...)
		if !item.Passed {
			report.Passed = false
		}
		if report.Stopped == "" {
			report.Stopped = item.Stopped
		}
	}
	return report
}

// Exclusive creation never replaces a previous result, even if it appears after preflight.
func saveReport(path string, report Report) error {
	data, err := json.MarshalIndent(struct {
		Version int    `json:"version"`
		Report  Report `json:"report"`
	}{1, report}, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 8*maxBytes {
		return fmt.Errorf("report exceeds 8 MiB")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(data, '\n'))
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func cli(args []string, out, errs io.Writer) int {
	return cliContext(context.Background(), args, out, errs)
}
func cliContext(ctx context.Context, args []string, out, errs io.Writer) int {
	if len(args) < 2 || len(args)%2 != 0 || args[0] != "run" {
		fmt.Fprintln(errs, "usage: probedeck run DECK.json [--deadline-ms N] [--concurrency N] [--output NEW.json]")
		return 2
	}
	workers := 1
	deadline := 0
	outputPath := ""
	seen := map[string]bool{}
	for i := 2; i < len(args); i += 2 {
		flag := args[i]
		if flag == "--output" && !seen[flag] {
			outputPath = args[i+1]
			if outputPath == "" {
				fmt.Fprintln(errs, "output path must not be empty")
				return 2
			}
			seen[flag] = true
			continue
		}
		value, err := strconv.Atoi(args[i+1])
		if err != nil || seen[flag] {
			fmt.Fprintln(errs, "invalid or duplicate run option")
			return 2
		}
		seen[flag] = true
		switch flag {
		case "--deadline-ms":
			if value < 1 || value > 3600000 {
				fmt.Fprintln(errs, "deadline must be 1 to 3600000 milliseconds")
				return 2
			}
			deadline = value
		case "--concurrency":
			if value < 1 || value > 32 {
				fmt.Fprintln(errs, "concurrency must be 1 to 32")
				return 2
			}
			workers = value
		default:
			fmt.Fprintln(errs, "unknown run option")
			return 2
		}
	}
	if outputPath != "" {
		if _, err := os.Lstat(outputPath); err == nil || !os.IsNotExist(err) {
			fmt.Fprintln(errs, "output must be a new file")
			return 2
		}
	}
	if deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(deadline)*time.Millisecond)
		defer cancel()
	}
	f, err := os.Open(args[1])
	if err != nil {
		fmt.Fprintln(errs, "cannot open deck")
		return 2
	}
	defer f.Close()
	d, err := readDeck(f)
	if err != nil {
		fmt.Fprintln(errs, "invalid deck:", err)
		return 2
	}
	report := runConcurrent(ctx, d, workers)
	if outputPath != "" {
		if err := saveReport(outputPath, report); err != nil {
			fmt.Fprintln(errs, "cannot save report:", err)
			return 2
		}
	}
	if err = json.NewEncoder(out).Encode(report); err != nil {
		fmt.Fprintln(errs, "cannot write report")
		return 2
	}
	if !report.Passed {
		return 1
	}
	return 0
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cliContext(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
