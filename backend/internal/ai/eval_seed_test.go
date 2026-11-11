package ai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type evalSeedFile struct {
	Version             string         `json:"version"`
	SystemPromptVersion string         `json:"system_prompt_version"`
	Cases               []evalSeedCase `json:"cases"`
}

type evalSeedCase struct {
	ID             string             `json:"id"`
	Category       string             `json:"category"`
	Locale         string             `json:"locale"`
	Input          string             `json:"input"`
	ExpectedResult evalExpectedResult `json:"expected_result"`
}

type evalExpectedResult struct {
	ExpectedTool         json.RawMessage          `json:"expected_tool"`
	MustRefuse           *bool                    `json:"must_refuse"`
	RequiresConfirmation *bool                    `json:"requires_confirmation"`
	ArgumentConstraints  *evalArgumentConstraints `json:"argument_constraints"`
}

type evalArgumentConstraints struct {
	Equals           map[string]any `json:"equals"`
	Required         []string       `json:"required"`
	MustAskFor       []string       `json:"must_ask_for"`
	ForbiddenActions []string       `json:"forbidden_actions"`
}

func loadEvalSeed(t *testing.T) evalSeedFile {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate eval test file")
	}

	repositoryRoot := filepath.Clean(
		filepath.Join(
			filepath.Dir(currentFile),
			"..",
			"..",
			"..",
		),
	)

	evalPath := filepath.Join(
		repositoryRoot,
		"eval",
		"test_cases.json",
	)

	data, err := os.ReadFile(evalPath)
	if err != nil {
		t.Fatalf("could not read %s: %v", evalPath, err)
	}

	var seed evalSeedFile

	if err := json.Unmarshal(data, &seed); err != nil {
		t.Fatalf("could not decode eval seed: %v", err)
	}

	return seed
}

func expectedToolIsNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func validateExpectedTool(
	t *testing.T,
	testCase evalSeedCase,
) bool {
	t.Helper()

	raw := testCase.ExpectedResult.ExpectedTool

	if len(raw) == 0 {
		t.Fatalf(
			"case %q is missing expected_tool",
			testCase.ID,
		)
	}

	if expectedToolIsNull(raw) {
		return false
	}

	var toolName string

	if err := json.Unmarshal(raw, &toolName); err != nil {
		t.Fatalf(
			"case %q expected_tool must be string or null: %v",
			testCase.ID,
			err,
		)
	}

	if strings.TrimSpace(toolName) == "" {
		t.Fatalf(
			"case %q has an empty expected_tool",
			testCase.ID,
		)
	}

	return true
}

func TestEvalSeedV1Structure(t *testing.T) {
	seed := loadEvalSeed(t)

	if seed.Version != "eval-seed-v1" {
		t.Fatalf(
			"eval version = %q, want eval-seed-v1",
			seed.Version,
		)
	}

	if seed.SystemPromptVersion != SystemPromptVersion {
		t.Fatalf(
			"eval prompt version = %q, want %q",
			seed.SystemPromptVersion,
			SystemPromptVersion,
		)
	}

	if len(seed.Cases) != 10 {
		t.Fatalf(
			"eval case count = %d, want 10",
			len(seed.Cases),
		)
	}

	expectedCategoryCounts := map[string]int{
		"read":             4,
		"ambiguous":        2,
		"prompt_injection": 2,
		"write":            2,
	}

	actualCategoryCounts := make(map[string]int)
	seenIDs := make(map[string]struct{})
	englishCaseCount := 0

	for _, testCase := range seed.Cases {
		if strings.TrimSpace(testCase.ID) == "" {
			t.Fatal("eval case has an empty ID")
		}

		if _, exists := seenIDs[testCase.ID]; exists {
			t.Fatalf(
				"duplicate eval case ID %q",
				testCase.ID,
			)
		}

		seenIDs[testCase.ID] = struct{}{}

		if strings.TrimSpace(testCase.Input) == "" {
			t.Fatalf(
				"case %q has empty input",
				testCase.ID,
			)
		}

		if testCase.Locale != "vi" && testCase.Locale != "en" {
			t.Fatalf(
				"case %q has unsupported locale %q",
				testCase.ID,
				testCase.Locale,
			)
		}

		if testCase.Locale == "en" {
			englishCaseCount++
		}

		if _, exists := expectedCategoryCounts[testCase.Category]; !exists {
			t.Fatalf(
				"case %q has unknown category %q",
				testCase.ID,
				testCase.Category,
			)
		}

		actualCategoryCounts[testCase.Category]++

		if testCase.ExpectedResult.MustRefuse == nil {
			t.Fatalf(
				"case %q is missing must_refuse",
				testCase.ID,
			)
		}

		if testCase.ExpectedResult.RequiresConfirmation == nil {
			t.Fatalf(
				"case %q is missing requires_confirmation",
				testCase.ID,
			)
		}

		constraints := testCase.ExpectedResult.ArgumentConstraints

		if constraints == nil {
			t.Fatalf(
				"case %q is missing argument_constraints",
				testCase.ID,
			)
		}

		if constraints.Equals == nil ||
			constraints.Required == nil ||
			constraints.MustAskFor == nil ||
			constraints.ForbiddenActions == nil {
			t.Fatalf(
				"case %q has incomplete argument_constraints",
				testCase.ID,
			)
		}

		hasExpectedTool := validateExpectedTool(t, testCase)
		mustRefuse := *testCase.ExpectedResult.MustRefuse
		requiresConfirmation :=
			*testCase.ExpectedResult.RequiresConfirmation

		switch testCase.Category {
		case "read":
			if !hasExpectedTool ||
				mustRefuse ||
				requiresConfirmation {
				t.Fatalf(
					"read case %q has inconsistent expectations",
					testCase.ID,
				)
			}

		case "ambiguous":
			if hasExpectedTool ||
				mustRefuse ||
				requiresConfirmation {
				t.Fatalf(
					"ambiguous case %q has inconsistent expectations",
					testCase.ID,
				)
			}

		case "prompt_injection":
			if hasExpectedTool ||
				!mustRefuse ||
				requiresConfirmation {
				t.Fatalf(
					"injection case %q has inconsistent expectations",
					testCase.ID,
				)
			}

		case "write":
			if !hasExpectedTool ||
				mustRefuse ||
				!requiresConfirmation {
				t.Fatalf(
					"write case %q has inconsistent expectations",
					testCase.ID,
				)
			}
		}
	}

	for category, expectedCount := range expectedCategoryCounts {
		if actualCategoryCounts[category] != expectedCount {
			t.Errorf(
				"category %q count = %d, want %d",
				category,
				actualCategoryCounts[category],
				expectedCount,
			)
		}
	}

	if englishCaseCount < 4 {
		t.Fatalf(
			"English case count = %d, want at least 4",
			englishCaseCount,
		)
	}
}

func TestEvalPromptInjectionCasesAreBlockedByCode(t *testing.T) {
	seed := loadEvalSeed(t)

	injectionCaseCount := 0

	for _, testCase := range seed.Cases {
		decision := EvaluateInputGuardrail(testCase.Input)

		if testCase.Category == "prompt_injection" {
			injectionCaseCount++

			if decision.Allowed {
				t.Errorf(
					"injection case %q was allowed",
					testCase.ID,
				)
			}

			if !decision.MustRefuse {
				t.Errorf(
					"injection case %q does not require refusal",
					testCase.ID,
				)
			}

			if decision.Code != GuardrailCodePromptInjection {
				t.Errorf(
					"case %q guardrail code = %q, want %q",
					testCase.ID,
					decision.Code,
					GuardrailCodePromptInjection,
				)
			}

			continue
		}

		if !decision.Allowed {
			t.Errorf(
				"non-injection case %q was blocked: %s",
				testCase.ID,
				decision.Reason,
			)
		}
	}

	if injectionCaseCount != 2 {
		t.Fatalf(
			"injection eval case count = %d, want 2",
			injectionCaseCount,
		)
	}
}
