package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type Dataset struct {
	Version             string     `json:"version"`
	SystemPromptVersion string     `json:"system_prompt_version"`
	Cases               []EvalCase `json:"cases"`
}

type EvalCase struct {
	ID             string          `json:"id"`
	Category       string          `json:"category"`
	Locale         string          `json:"locale"`
	Input          string          `json:"input"`
	ExpectedResult json.RawMessage `json:"expected_result"`
}

type CaseResult struct {
	ID             string          `json:"id"`
	Category       string          `json:"category"`
	Locale         string          `json:"locale"`
	Input          string          `json:"input"`
	ExpectedResult json.RawMessage `json:"expected_result"`
	SessionID      string          `json:"session_id"`
	HTTPStatus     int             `json:"http_status"`
	LatencyMS      int64           `json:"latency_ms"`
	Response       json.RawMessage `json:"response,omitempty"`
	ResponseText   string          `json:"response_text,omitempty"`
	Error          string          `json:"error,omitempty"`
	ReviewStatus   string          `json:"review_status"`
}

type Report struct {
	DatasetVersion string       `json:"dataset_version"`
	PromptVersion  string       `json:"prompt_version"`
	Commit         string       `json:"commit"`
	Provider       string       `json:"provider"`
	Model          string       `json:"model"`
	DataMode       string       `json:"data_mode"`
	Endpoint       string       `json:"endpoint"`
	StartedAt      string       `json:"started_at"`
	FinishedAt     string       `json:"finished_at"`
	Results        []CaseResult `json:"results"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	endpoint := flag.String(
		"endpoint",
		"http://localhost:8080/api/v1/chat",
		"Chat API endpoint",
	)
	casesPath := flag.String("cases", "eval/test_cases.json", "Dataset path")
	outputPath := flag.String("out", "eval/eval_report.md", "Report path")
	wallet := flag.String("wallet", "", "Public Fuji wallet address")
	provider := flag.String("provider", "", "Provider used by the running backend")
	model := flag.String("model", "", "Model used by the running backend")
	dataMode := flag.String("data-mode", "", "Data mode used by the running backend")
	interval := flag.Duration(
		"interval",
		15*time.Second,
		"Minimum pause between evaluation cases",
	)
	flag.Parse()

	if !validAddress(*wallet) {
		return fmt.Errorf("-wallet must be a public 0x address with 40 hex digits")
	}
	if *provider == "" || *model == "" || *dataMode == "" {
		return fmt.Errorf("-provider, -model and -data-mode are required")
	}

	data, err := os.ReadFile(*casesPath)
	if err != nil {
		return fmt.Errorf("read dataset: %w", err)
	}

	var dataset Dataset
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &dataset); err != nil {
		return fmt.Errorf("decode dataset: %w", err)
	}
	if len(dataset.Cases) == 0 {
		return fmt.Errorf("dataset contains no cases")
	}

	seen := make(map[string]bool)
	for _, testCase := range dataset.Cases {
		if testCase.ID == "" || seen[testCase.ID] {
			return fmt.Errorf("empty or duplicate case ID: %q", testCase.ID)
		}
		seen[testCase.ID] = true
		if testCase.Locale != "vi" && testCase.Locale != "en" {
			return fmt.Errorf("case %q has invalid locale", testCase.ID)
		}
		if strings.TrimSpace(testCase.Input) == "" {
			return fmt.Errorf("case %q has empty input", testCase.ID)
		}
	}

	commit := "unknown"
	if value, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		commit = strings.TrimSpace(string(value))
	}

	started := time.Now().UTC()
	report := Report{
		DatasetVersion: dataset.Version,
		PromptVersion:  dataset.SystemPromptVersion,
		Commit:         commit,
		Provider:       *provider,
		Model:          *model,
		DataMode:       *dataMode,
		Endpoint:       *endpoint,
		StartedAt:      started.Format(time.RFC3339Nano),
		Results:        make([]CaseResult, 0, len(dataset.Cases)),
	}

	client := &http.Client{
		Timeout: 70 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for index, testCase := range dataset.Cases {
		if index > 0 {
			time.Sleep(*interval)
		}
		sessionID := fmt.Sprintf("eval-%d-%d", started.UnixNano(), index)
		var result CaseResult

		if testCase.Category == "upstream_error" {
			result = CaseResult{
				ID:             testCase.ID,
				Category:       testCase.Category,
				Locale:         testCase.Locale,
				Input:          testCase.Input,
				ExpectedResult: testCase.ExpectedResult,
				SessionID:      sessionID,
				ReviewStatus:   "skipped",
				Error:          "controlled upstream fault injection is not configured",
			}
		} else {
			result = executeCase(client, *endpoint, *wallet, sessionID, testCase)
		}
		report.Results = append(report.Results, result)
		report.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)

		// Save after each case so completed responses survive interruption.
		if err := saveReport(*outputPath, report); err != nil {
			return err
		}

		fmt.Printf(
			"[%d/%d] %s locale=%s HTTP=%d latency=%dms review=%s\n",
			index+1, len(dataset.Cases), result.ID, result.Locale,
			result.HTTPStatus, result.LatencyMS, result.ReviewStatus,
		)
		var envelope struct {
			Error *struct {
				Code string `json:"code"`
			} `json:"error"`
		}

		if len(result.Response) > 0 {
			if err := json.Unmarshal(result.Response, &envelope); err == nil {
				if envelope.Error != nil &&
					envelope.Error.Code == "LLM_RATE_LIMITED" {
					return fmt.Errorf(
						"stopped at %s because the provider is rate limited; completed results saved to %s",
						result.ID,
						*outputPath,
					)
				}
			}
		}
	}

	fmt.Printf("Saved %s\n", *outputPath)
	return nil
}

func executeCase(
	client *http.Client,
	endpoint string,
	wallet string,
	sessionID string,
	testCase EvalCase,
) CaseResult {
	result := CaseResult{
		ID:             testCase.ID,
		Category:       testCase.Category,
		Locale:         testCase.Locale,
		Input:          testCase.Input,
		ExpectedResult: testCase.ExpectedResult,
		SessionID:      sessionID,
		ReviewStatus:   "needs_review",
	}

	payload, err := json.Marshal(map[string]any{
		"session_id":     sessionID,
		"wallet_address": wallet,
		"chain_id":       43113,
		"locale":         testCase.Locale,
		"message":        testCase.Input,
	})
	if err != nil {
		result.Error = err.Error()
		result.ReviewStatus = "execution_error"
		return result
	}

	request, err := http.NewRequest(
		http.MethodPost,
		endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		result.Error = err.Error()
		result.ReviewStatus = "execution_error"
		return result
	}

	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set(
		"Idempotency-Key",
		fmt.Sprintf("eval-%d", time.Now().UnixNano()),
	)

	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		result.LatencyMS = time.Since(started).Milliseconds()
		result.Error = err.Error()
		result.ReviewStatus = "execution_error"
		return result
	}
	defer response.Body.Close()

	result.HTTPStatus = response.StatusCode
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	result.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		result.ReviewStatus = "execution_error"
		return result
	}
	if len(body) > 1024*1024 {
		result.Error = "response exceeds 1 MiB"
		result.ReviewStatus = "execution_error"
		return result
	}
	if !json.Valid(body) {
		result.ResponseText = string(body)
		result.Error = "response is not valid JSON"
		result.ReviewStatus = "execution_error"
		return result
	}

	result.Response = json.RawMessage(body)
	return result
}

func saveReport(path string, report Report) error {
	var text strings.Builder
	fmt.Fprintln(&text, "# DeFi Copilot evaluation run")
	fmt.Fprintf(&text, "\nDataset %s\n\n", report.DatasetVersion)
	fmt.Fprintf(&text, "Provider %s, model %s, data mode %s\n\n",
		report.Provider, report.Model, report.DataMode)
	fmt.Fprintln(&text, "Provider/model/data mode are supplied by the operator and must match the backend.")
	fmt.Fprintln(&text, "\nResponses require review against expected_result. HTTP success alone receives no pass score.")
	fmt.Fprintln(&text, "\nUnsafe blocking, clarification quality, numeric grounding and forbidden claims require review.")
	fmt.Fprintln(&text, "\n| Case | Language | HTTP | Latency ms | Review |")
	fmt.Fprintln(&text, "|---|---|---:|---:|---|")

	latencies := make([]int64, 0, len(report.Results))
	for _, result := range report.Results {
		fmt.Fprintf(&text, "| %s | %s | %d | %d | %s |\n",
			result.ID, result.Locale, result.HTTPStatus,
			result.LatencyMS, result.ReviewStatus)
		if result.ReviewStatus != "skipped" {
			latencies = append(latencies, result.LatencyMS)
		}
	}

	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})
	if len(latencies) > 0 {
		fmt.Fprintf(&text, "\nEnd-to-end p50 %d ms, p95 %d ms, including failed requests.\n",
			percentile(latencies, 50), percentile(latencies, 95))
	}

	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	// Escape backticks originating from model output inside the JSON block.
	raw = bytes.ReplaceAll(raw, []byte("`"), []byte(`\u0060`))
	fmt.Fprintf(&text, "\n## Raw results\n\n```json\n%s\n```\n", raw)

	if err := os.WriteFile(path, []byte(text.String()), 0644); err != nil {
		return fmt.Errorf("save report: %w", err)
	}
	return nil
}

func percentile(values []int64, percentage int) int64 {
	index := (len(values)*percentage + 99) / 100
	return values[index-1]
}

func validAddress(value string) bool {
	if len(value) != 42 || !strings.HasPrefix(value, "0x") {
		return false
	}
	for _, character := range value[2:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}
