package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Serebr1k-code/Writeups-MCP/internal/indexer"
)

func main() {
	var (
		source       = flag.String("source", "", "Source directory with writeup files")
		dbPath       = flag.String("db", "", "Target SQLite database path")
		minChars     = flag.Int("min-chars", 300, "Minimum cleaned text length to index")
		workers      = flag.Int("workers", runtime.NumCPU(), "Number of file processing workers")
		commitEvery  = flag.Int("commit-every", 200, "Commit transaction every N mutations")
		extensions   = flag.String("extensions", ".md,.txt,.rst,.html", "Comma-separated file extensions")
		prune        = flag.Bool("prune-deleted", true, "Delete DB rows for files no longer present on disk")
		showProgress = flag.Bool("progress", true, "Show live progress bar")
	)
	flag.Parse()

	if strings.TrimSpace(*source) == "" || strings.TrimSpace(*dbPath) == "" {
		flag.Usage()
		os.Exit(2)
	}

	started := time.Now()
	stats, err := indexer.RunWithProgress(context.Background(), indexer.Options{
		SourceDir:    *source,
		DBPath:       *dbPath,
		MinChars:     *minChars,
		Workers:      *workers,
		CommitEvery:  *commitEvery,
		Extensions:   splitCSV(*extensions),
		PruneDeleted: *prune,
	}, newProgressPrinter(*showProgress))
	if err != nil {
		log.Fatalf("index build failed: %v", err)
	}
	clearProgressLine(*showProgress)

	fmt.Printf("Indexed source: %s\n", *source)
	fmt.Printf("Database: %s\n", *dbPath)
	fmt.Printf("Candidates: %d\n", stats.Candidates)
	fmt.Printf("Processed: %d\n", stats.Processed)
	fmt.Printf("Indexed new: %d\n", stats.Indexed)
	fmt.Printf("Updated: %d\n", stats.Updated)
	fmt.Printf("Skipped cached: %d\n", stats.SkippedCached)
	fmt.Printf("Skipped short: %d\n", stats.SkippedShort)
	fmt.Printf("Skipped duplicates: %d\n", stats.SkippedDup)
	fmt.Printf("Deleted missing: %d\n", stats.DeletedMissing)
	fmt.Printf("Read errors: %d\n", stats.ReadErrors)
	fmt.Printf("Duration: %s\n", time.Since(started).Round(time.Millisecond))
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func newProgressPrinter(enabled bool) indexer.ProgressWriter {
	if !enabled {
		return nil
	}

	return func(stats indexer.Stats) {
		total := stats.Candidates
		processed := stats.Processed
		percent := 0.0
		if total > 0 {
			percent = float64(processed) / float64(total) * 100
		}

		barWidth := 28
		filled := 0
		if total > 0 {
			filled = int(float64(processed) / float64(total) * float64(barWidth))
			if filled > barWidth {
				filled = barWidth
			}
		}

		bar := strings.Repeat("=", filled)
		if filled < barWidth {
			bar += ">" + strings.Repeat(" ", barWidth-filled-1)
		}
		if filled == barWidth {
			bar = strings.Repeat("=", barWidth)
		}

		elapsed := time.Since(stats.StartedAt).Round(time.Second)
		fmt.Fprintf(
			os.Stderr,
			"\r[%s] %6.2f%% %d/%d | new:%d upd:%d cached:%d short:%d dup:%d err:%d | %s",
			bar,
			percent,
			processed,
			total,
			stats.Indexed,
			stats.Updated,
			stats.SkippedCached,
			stats.SkippedShort,
			stats.SkippedDup,
			stats.ReadErrors,
			elapsed,
		)
	}
}

func clearProgressLine(enabled bool) {
	if !enabled {
		return
	}
	fmt.Fprint(os.Stderr, "\r"+strings.Repeat(" ", 140)+"\r")
}
