package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ArnobKumarSaha/rag/internal/embed"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "embed":
		err = runEmbed(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rag:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: rag embed <text> <text> [text...]")
	os.Exit(2)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func runEmbed(texts []string) error {
	if len(texts) < 2 {
		return errors.New("embed needs at least 2 texts to compare")
	}
	host := envOr("OLLAMA_HOST", "http://localhost:11434")
	model := envOr("RAG_EMBED_MODEL", "nomic-embed-text")

	res, err := embed.Embed(context.Background(), host, model, texts)
	if err != nil {
		return err
	}

	fmt.Printf("model %s, %d vectors, %d dims, %d input tokens\n\n",
		res.Model, len(res.Embeddings), len(res.Embeddings[0]), res.PromptEvalCount)
	for i, v := range res.Embeddings {
		fmt.Printf("[%d] norm=%.3f first=%.3f %q\n", i, embed.Norm(v), v[:min(4, len(v))], texts[i])
	}

	fmt.Print("\ncosine")
	for j := range texts {
		fmt.Printf("%8s", fmt.Sprintf("[%d]", j))
	}
	fmt.Println()
	for i, a := range res.Embeddings {
		fmt.Printf("%-6s", fmt.Sprintf("[%d]", i))
		for _, b := range res.Embeddings {
			c, err := embed.Cosine(a, b)
			if err != nil {
				return fmt.Errorf("cosine [%d]: %w", i, err)
			}
			fmt.Printf("%8.3f", c)
		}
		fmt.Println()
	}
	return nil
}
