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
	"time"
)

const maxBytes = 1024 * 1024

type JSONAssertion struct {
	Pointer string          `json:"pointer"`
	Equals  json.RawMessage `json:"equals"`
}

type Check struct {
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

func readDeck(r io.Reader) (Deck, error) {
	var d Deck
	b, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return d, err
	}
	if len(b) > maxBytes {
		return d, fmt.Errorf("deck exceeds 1 MiB")
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
		for _, a := range c.JSON {
			if _, err := pointerParts(a.Pointer); err != nil || len(a.Equals) == 0 {
				return d, fmt.Errorf("%s: JSON assertion requires a valid pointer and equals value", c.Name)
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
		} else if e != nil || !reflect.DeepEqual(value, expected) {
			problems = append(problems, "JSON value mismatch: "+a.Pointer)
		}
	}
	return problems
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
			req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, c.URL, nil)
			if err != nil {
				result.Problems = append(result.Problems, "invalid request")
				return
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

func cli(args []string, out, errs io.Writer) int {
	return cliContext(context.Background(), args, out, errs)
}
func cliContext(ctx context.Context, args []string, out, errs io.Writer) int {
	if (len(args) != 2 && len(args) != 4) || args[0] != "run" {
		fmt.Fprintln(errs, "usage: probedeck run DECK.json [--deadline-ms N]")
		return 2
	}
	if len(args) == 4 {
		ms, err := strconv.Atoi(args[3])
		if args[2] != "--deadline-ms" || err != nil || ms < 1 || ms > 3600000 {
			fmt.Fprintln(errs, "deadline must be 1 to 3600000 milliseconds")
			return 2
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
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
	report := runDeck(ctx, d)
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
