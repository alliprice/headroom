package provider

import (
	"encoding/json"
	"os"
	"testing"
)

// TestGeminiFetchIntegration exercises the real Gemini API. It is skipped
// automatically when credentials are absent, so it's safe in CI.
//
// Run explicitly with:
//
//	go test ./internal/provider/ -run TestGeminiFetchIntegration -v
func TestGeminiFetchIntegration(t *testing.T) {
	if _, err := os.Stat(geminiCredsPath()); err != nil {
		t.Skipf("skipping: no Gemini credentials at %s", geminiCredsPath())
	}

	// Reset cached project ID so we test the full flow.
	geminiProjectID = ""

	data, err := fetchGeminiAPI()
	if err != nil {
		t.Fatalf("fetchGeminiAPI() error: %v", err)
	}
	if data == nil {
		t.Fatal("fetchGeminiAPI() returned nil data")
	}

	// Dump the raw response so we can see the actual shape.
	raw, _ := json.MarshalIndent(data, "", "  ")
	t.Logf("retrieveUserQuota response:\n%s", raw)

	// The response must contain a "buckets" array.
	bucketsRaw, ok := data["buckets"]
	if !ok {
		t.Fatal("response missing 'buckets' key")
	}
	buckets, ok := bucketsRaw.([]any)
	if !ok {
		t.Fatalf("'buckets' is %T, want []any", bucketsRaw)
	}
	if len(buckets) == 0 {
		t.Fatal("'buckets' array is empty")
	}

	// Validate bucket structure.
	for i, b := range buckets {
		bucket, ok := b.(map[string]any)
		if !ok {
			t.Errorf("bucket[%d] is %T, want map[string]any", i, b)
			continue
		}

		// Every bucket should have a modelId string.
		if _, ok := bucket["modelId"].(string); !ok {
			t.Errorf("bucket[%d] missing or non-string 'modelId': %v", i, bucket["modelId"])
		}

		// remainingFraction should be a float64 when present.
		if rf, exists := bucket["remainingFraction"]; exists {
			if _, ok := rf.(float64); !ok {
				t.Errorf("bucket[%d] 'remainingFraction' is %T, want float64", i, rf)
			}
		}

		// Log each bucket's fields for debugging.
		bj, _ := json.Marshal(bucket)
		t.Logf("  bucket[%d]: %s", i, bj)
	}
}

// TestGeminiLoadCodeAssistIntegration tests just the loadCodeAssist call
// to verify we extract the project ID correctly.
func TestGeminiLoadCodeAssistIntegration(t *testing.T) {
	if _, err := os.Stat(geminiCredsPath()); err != nil {
		t.Skipf("skipping: no Gemini credentials at %s", geminiCredsPath())
	}

	creds, err := readGeminiCreds()
	if err != nil {
		t.Fatalf("readGeminiCreds() error: %v", err)
	}

	if err := refreshGeminiToken(creds); err != nil {
		t.Fatalf("refreshGeminiToken() error: %v", err)
	}

	payload := map[string]any{
		"metadata": map[string]any{
			"ideType":    "IDE_UNSPECIFIED",
			"platform":   "PLATFORM_UNSPECIFIED",
			"pluginType": "GEMINI",
		},
	}
	data, err := geminiAPIPost(creds.AccessToken, "loadCodeAssist", payload)
	if err != nil {
		t.Fatalf("loadCodeAssist error: %v", err)
	}

	raw, _ := json.MarshalIndent(data, "", "  ")
	t.Logf("loadCodeAssist response:\n%s", raw)

	pid, ok := data["cloudaicompanionProject"].(string)
	if !ok || pid == "" {
		t.Fatalf("no 'cloudaicompanionProject' in response; keys present: %v", mapKeys(data))
	}
	t.Logf("project ID: %s", pid)
}

// mapKeys returns the top-level keys of a map for diagnostic logging.
func mapKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
