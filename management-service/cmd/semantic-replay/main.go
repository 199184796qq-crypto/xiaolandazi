// semantic-replay evaluates labeled live-language pairs against the configured
// embedding provider. Input is JSONL with query, candidate, same, and scene.
// It prints aggregate scores only and never echoes audience text.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"livecompanion/management/internal/semantic"
)

type pair struct {
	Scene     string `json:"scene"`
	Query     string `json:"query"`
	Candidate string `json:"candidate"`
	Same      bool   `json:"same"`
}

type scoredPair struct {
	Same  bool
	Score float64
}

func main() {
	input := flag.String("input", "", "path to labeled JSONL pairs")
	scene := flag.String("scene", "", "optional scene filter")
	threshold := flag.Float64("threshold", 0, "optional current threshold, 0 to report sweep only")
	maxPairs := flag.Int("max-pairs", 500, "maximum labeled pairs to read")
	flag.Parse()
	if strings.TrimSpace(*input) == "" || *maxPairs <= 0 || *maxPairs > 5000 {
		log.Fatal("--input is required and --max-pairs must be 1..5000")
	}
	file, err := os.Open(*input)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	items := make([]pair, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var item pair
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			log.Fatalf("invalid pair on line %d: %v", lineNumber, err)
		}
		if *scene != "" && item.Scene != *scene {
			continue
		}
		item.Query = strings.TrimSpace(item.Query)
		item.Candidate = strings.TrimSpace(item.Candidate)
		if item.Query == "" || item.Candidate == "" {
			log.Fatal("query and candidate must be nonempty")
		}
		items = append(items, item)
		if len(items) >= *maxPairs {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	if len(items) == 0 {
		log.Fatal("no labeled pairs selected")
	}
	provider := semantic.NewFromEnv()
	if !provider.Enabled() {
		log.Fatal("SEMANTIC_ENABLED and DASHSCOPE_API_KEY must be configured")
	}
	texts := make([]string, 0, len(items)*2)
	indexes := make(map[string]int, len(items)*2)
	add := func(text string) {
		if _, exists := indexes[text]; !exists {
			indexes[text] = len(texts)
			texts = append(texts, text)
		}
	}
	for _, item := range items {
		add(item.Query)
		add(item.Candidate)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	vectors, err := provider.Embed(ctx, texts)
	if err != nil {
		log.Fatalf("embed replay pairs: %v", err)
	}
	scores := make([]scoredPair, 0, len(items))
	for _, item := range items {
		scores = append(scores, scoredPair{Same: item.Same,
			Score: semantic.CosineSimilarity(vectors[indexes[item.Query]], vectors[indexes[item.Candidate]])})
	}
	fmt.Printf("model=%s scene=%s pairs=%d unique_texts=%d\n", provider.Model(), *scene, len(items), len(texts))
	if *threshold > 0 {
		printEvaluation(scores, *threshold)
	}
	for _, candidate := range []float64{0.30, 0.35, 0.40, 0.45, 0.50, 0.55, 0.60, 0.65, 0.70, 0.75, 0.80} {
		if *threshold > 0 && math.Abs(candidate-*threshold) < 0.0001 {
			continue
		}
		printEvaluation(scores, candidate)
	}
}

func printEvaluation(scores []scoredPair, threshold float64) {
	var tp, fp, tn, fn int
	for _, pair := range scores {
		predicted := pair.Score >= threshold
		switch {
		case predicted && pair.Same:
			tp++
		case predicted && !pair.Same:
			fp++
		case !predicted && pair.Same:
			fn++
		default:
			tn++
		}
	}
	precision, recall := 0.0, 0.0
	if tp+fp > 0 {
		precision = float64(tp) / float64(tp+fp)
	}
	if tp+fn > 0 {
		recall = float64(tp) / float64(tp+fn)
	}
	fmt.Printf("threshold=%.2f tp=%d fp=%d tn=%d fn=%d precision=%.3f recall=%.3f\n",
		threshold, tp, fp, tn, fn, precision, recall)
}
