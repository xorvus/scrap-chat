package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"time"

	"github.com/xorvus/scrap-chat/pkg/scrapchat"
	"github.com/xorvus/scrap-chat/types"
)

var (
	duration   = flag.Int("duration", 60, "benchmark duration in seconds")
	tracefile  = flag.String("trace", "", "write trace to file")
	memprofile = flag.String("memprofile", "", "write memory profile to file")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
)

func main() {
	flag.Parse()

	if err := validateArguments(); err != nil {
		log.Fatal(err)
	}

	if err := setupTracing(); err != nil {
		log.Fatal(err)
	}
	defer cleanupTracing()

	if err := setupCPUProfile(); err != nil {
		log.Fatal(err)
	}
	defer cleanupCPUProfile()

	channelURL := flag.Arg(0)

	chat, err := scrapchat.New("youtube", false)
	if err != nil {
		log.Fatalf("Failed to create scraper: %v", err)
	}

	data, err := chat.FetchLiveChat(channelURL)
	if err != nil {
		log.Fatalf("Error fetching live chat: %v", err)
	}

	result := runBenchmark(data)

	printResults(result)

	if err := writeMemoryProfile(); err != nil {
		log.Fatal(err)
	}
}

func validateArguments() error {
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: benchmark [options] <channel_url>")
		flag.PrintDefaults()
		return fmt.Errorf("channel URL is required")
	}
	return nil
}

func setupTracing() error {
	if *tracefile == "" {
		return nil
	}

	f, err := os.Create(*tracefile)
	if err != nil {
		return err
	}

	if err := trace.Start(f); err != nil {
		if err := f.Close(); err != nil {
			log.Printf("Error closing trace file: %v", err)
		}
		return err
	}

	traceFile = f
	return nil
}

func cleanupTracing() {
	if traceFile != nil {
		trace.Stop()
		if err := traceFile.Close(); err != nil {
			log.Printf("Error closing trace file: %v", err)
		}
	}
}

func setupCPUProfile() error {
	if *cpuprofile == "" {
		return nil
	}

	f, err := os.Create(*cpuprofile)
	if err != nil {
		return err
	}

	if err := pprof.StartCPUProfile(f); err != nil {
		if err := f.Close(); err != nil {
			log.Printf("Error closing CPU profile file: %v", err)
		}
		return fmt.Errorf("error starting CPU profile: %w", err)
	}

	cpuProfileFile = f
	return nil
}

func cleanupCPUProfile() {
	if cpuProfileFile != nil {
		pprof.StopCPUProfile()
		if err := cpuProfileFile.Close(); err != nil {
			log.Printf("Error closing CPU profile file: %v", err)
		}
	}
}

func runBenchmark(data <-chan *types.LiveChatMessage) BenchmarkResult {
	log.Printf("Starting benchmark for %d seconds", *duration)

	startMem := getMemStats()
	startTime := time.Now()

	msgCount := collectMessages(data)

	endTime := time.Now()
	endMem := getMemStats()
	duration := endTime.Sub(startTime)

	return BenchmarkResult{
		Duration:     duration,
		MessageCount: msgCount,
		StartMem:     startMem,
		EndMem:       endMem,
		GoRoutines:   runtime.NumGoroutine(),
	}
}

func collectMessages(data <-chan *types.LiveChatMessage) int {
	msgCount := 0
	timeout := time.After(time.Duration(*duration) * time.Second)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-data:
			if !ok {
				return msgCount
			}
			msgCount++
			_ = msg

		case <-ticker.C:
			_ = getMemStats()

		case <-timeout:
			return msgCount
		}
	}
}

func printResults(result BenchmarkResult) {
	log.Printf("\n=== Benchmark Results ===")
	log.Printf("Duration: %v", result.Duration)
	log.Printf("Messages: %d", result.MessageCount)
	log.Printf("Msg/sec: %.2f", float64(result.MessageCount)/result.Duration.Seconds())

	log.Printf("\n=== Memory Usage ===")
	log.Printf("Start: %.2f MB", result.StartMem.AllocMB)
	log.Printf("End: %.2f MB", result.EndMem.AllocMB)
	log.Printf("Diff: %.2f MB", result.EndMem.AllocMB-result.StartMem.AllocMB)
	log.Printf("GC Runs: %d", result.EndMem.NumGC-result.StartMem.NumGC)
	log.Printf("Goroutines: %d", result.GoRoutines)
}

func writeMemoryProfile() error {
	if *memprofile == "" {
		return nil
	}

	f, err := os.Create(*memprofile)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("Error closing memory profile file: %v", err)
		}
	}()

	runtime.GC()
	if err := pprof.WriteHeapProfile(f); err != nil {
		return err
	}

	return nil
}

type BenchmarkResult struct {
	Duration     time.Duration
	MessageCount int
	StartMem     memStat
	EndMem       memStat
	GoRoutines   int
}

var (
	traceFile      *os.File
	cpuProfileFile *os.File
)

type memStat struct {
	AllocMB float64
	NumGC   uint32
}

func getMemStats() memStat {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return memStat{
		AllocMB: float64(m.Alloc) / 1024 / 1024,
		NumGC:   m.NumGC,
	}
}

