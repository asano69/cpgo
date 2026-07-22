package main

import (
	"fmt"
	"sync"
	"time"
)

// startProgressPrinter renders a single updating status line until the
// returned stop function is called. Calling stop more than once is safe.
func startProgressPrinter(p *Progress, opts Options) func() {
	if opts.DryRun {
		fmt.Println("dry run: no files will be modified")
	}

	done := make(chan struct{})
	var once sync.Once
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				printProgressLine(p)
			case <-done:
				return
			}
		}
	}()

	return func() {
		once.Do(func() { close(done) })
		wg.Wait()
	}
}

func printProgressLine(p *Progress) {
	doneBytes := p.DoneBytes.Load()
	pct := 0.0
	if p.TotalBytes > 0 {
		pct = float64(doneBytes) / float64(p.TotalBytes) * 100
	}
	fmt.Printf("\r%6.1f%%  %s / %s  (%d/%d files, %d failed)   ",
		pct, humanBytes(doneBytes), humanBytes(p.TotalBytes),
		p.DoneFiles.Load(), p.TotalFiles, p.Failed.Load())
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
