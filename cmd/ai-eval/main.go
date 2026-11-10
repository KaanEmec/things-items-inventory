// Command ai-eval runs the AI test set against a real endpoint and prints,
// per field, how often the answer was right, empty or wrong (J6.1). It is a
// manual tool for when a prompt or model changes, not part of CI. See
// README.md for how to build the test set.
//
//	go run ./cmd/ai-eval -cases ~/inventory-ai-cases [-mode strict] [-only sony] [-v]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/ai"
)

const casesHelp = `ai-eval needs a directory of test cases: -cases DIR (or make ai-eval CASES=DIR).

Put in it, for every case <name>:
  <name>.jpg            a photo (item, label, receipt or open box), or
  <name>.txt            text (typed rows, pasted list, or a CSV file)
  <name>.expected.json  which task to run, the household context, and the
                        expected answer per field (see cmd/ai-eval/README.md)
Optional extra photos for an item: <name>.label.jpg, <name>.receipt.jpg.

Keep private photos outside the repository, e.g. ~/inventory-ai-cases.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ai-eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("cases", "", "directory of test cases (required)")
	mode := fs.String("mode", "auto", "structured output mode: auto, strict, json_object or prompt")
	only := fs.String("only", "", "run only cases whose name contains this")
	verbose := fs.Bool("v", false, "print every answer and every wrong field")
	timeout := fs.Duration("timeout", 3*time.Minute, "time limit per case")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dir == "" && fs.NArg() > 0 {
		*dir = fs.Arg(0)
	}
	if strings.TrimSpace(*dir) == "" {
		fmt.Fprint(stderr, casesHelp)
		return 2
	}
	cases, err := loadCases(*dir, *only)
	if err != nil {
		fmt.Fprintf(stderr, "ai-eval: %v\n\n%s", err, casesHelp)
		return 2
	}
	m := ai.Mode(*mode)
	switch m {
	case ai.ModeAuto, ai.ModeStrict, ai.ModeJSONObject, ai.ModePrompt:
	default:
		fmt.Fprintf(stderr, "ai-eval: unknown -mode %q\n", *mode)
		return 2
	}
	vision, text, err := endpoints(m)
	if err != nil {
		fmt.Fprintf(stderr, "ai-eval: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "vision: %s at %s · text: %s at %s · mode %s · %d cases\n\n",
		vision.Model, vision.BaseURL, text.Model, text.BaseURL, m, len(cases))

	client := ai.New(nil)
	sc := newScore()
	fmt.Fprintf(stdout, "%-28s %-15s %-12s %13s %7s  %s\n", "case", "task", "mode", "tokens in/out", "secs", "result")
	for _, c := range cases {
		cfg := text
		if c.vision() {
			cfg = vision
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		start := time.Now()
		r := c.run(ctx, client, cfg)
		cancel()
		secs := time.Since(start).Seconds()
		status := "ok"
		if r.err != nil {
			status = "FAILED: " + oneLine(r.err.Error(), 200)
			var ae *ai.Error
			if errors.As(r.err, &ae) && ae.Kind != ai.KindInvalidOutput {
				status = "ERROR: " + oneLine(r.err.Error(), 200)
			}
		}
		wrong := sc.add(c, r)
		fmt.Fprintf(stdout, "%-28s %-15s %-12s %6d/%-6d %7.1f  %s\n", clip(c.name, 28), c.task, r.res.Mode,
			r.res.Usage.PromptTokens, r.res.Usage.CompletionTokens, secs, status)
		if *verbose {
			if len(r.res.JSON) > 0 {
				fmt.Fprintf(stdout, "    answer: %s\n", r.res.JSON)
			}
			for _, w := range wrong {
				fmt.Fprintf(stdout, "    %s\n", w)
			}
		}
	}
	fmt.Fprintln(stdout)
	sc.print(stdout)
	return 0
}

// endpoints reads the vision and text endpoints from the same environment
// variables the server uses. The text endpoint falls back to the vision one.
func endpoints(m ai.Mode) (vision, text ai.Config, err error) {
	read := func(role string) ai.Config {
		p := "INVENTORY_AI_" + role + "_"
		c := ai.Config{BaseURL: os.Getenv(p + "BASE_URL"), Token: os.Getenv(p + "TOKEN"), Model: os.Getenv(p + "MODEL"), Mode: m}
		if c.BaseURL == "" {
			c.BaseURL = ai.DefaultBaseURL
		}
		return c
	}
	vision, text = read("VISION"), read("TEXT")
	if text.Model == "" {
		text = vision
	}
	if vision.Model == "" {
		return vision, text, errors.New("set INVENTORY_AI_VISION_MODEL and INVENTORY_AI_VISION_TOKEN (and _BASE_URL for an endpoint other than OpenRouter); INVENTORY_AI_TEXT_* is optional and defaults to the vision endpoint")
	}
	return vision, text, nil
}

func oneLine(s string, n int) string {
	return clip(strings.Join(strings.Fields(s), " "), n)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
