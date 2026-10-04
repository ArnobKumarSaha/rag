package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type request struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type Response struct {
	Model           string      `json:"model"`
	Embeddings      [][]float64 `json:"embeddings"`
	PromptEvalCount int         `json:"prompt_eval_count"`
}

func Embed(ctx context.Context, host, model string, inputs []string) (*Response, error) {
	body, err := json.Marshal(request{Model: model, Input: inputs})
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(host, "/") + "/api/embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("POST %s: %s: %s", url, resp.Status, raw)
	}

	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode %s: %w", url, err)
	}
	if len(out.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("sent %d inputs, got %d embeddings", len(inputs), len(out.Embeddings))
	}
	return &out, nil
}
