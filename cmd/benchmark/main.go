package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"time"

	"github.com/xorvus/scrap-chat/internal/logger"
	"github.com/xorvus/scrap-chat/pkg/scrapchat"
	"github.com/xorvus/scrap-chat/types"
)

var (
	duration   = flag.Int("duration", 60, "benchmark duration in seconds")
	tracefile  = flag.String("trace", "", "write trace to file")
	memprofile = flag.String("memprofile", "", "write memory profile to file")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
	log        = logger.New("BENCHMARK", false)
)

func main() {
	flag.Parse()

	if err := validateArguments(); err != nil {
		log.Fatal("%v", err)
	}

	if err := setupTracing(); err != nil {
		log.Fatal("%v", err)
	}
	defer cleanupTracing()

	if err := setupCPUProfile(); err != nil {
		log.Fatal("%v", err)
	}
	defer cleanupCPUProfile()

	channelURL := flag.Arg(0)

	chat, err := scrapchat.New("youtube", false)
	if err != nil {
		log.Fatal("Failed to create scraper: %v", err)
	}

	data, err := chat.FetchLiveChat(channelURL)
	if err != nil {
		log.Fatal("Error fetching live chat: %v", err)
	}

	result := runBenchmark(data)

	printResults(result)

	if err := writeMemoryProfile(); err != nil {
		log.Fatal("%v", err)
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
		return fmt.Errorf("failed to create trace file: %w", err)
	}

	if err := trace.Start(f); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			log.Warn("Error closing trace file: %v", closeErr)
		}
		return fmt.Errorf("failed to start tracing: %w", err)
	}

	traceFile = f
	log.Info("Tracing started, output: %s", *tracefile)
	return nil
}

func cleanupTracing() {
	if traceFile != nil {
		trace.Stop()
		if err := traceFile.Close(); err != nil {
			log.Error("Error closing trace file: %v", err)
		}
		log.Info("Tracing stopped")
	}
}

func setupCPUProfile() error {
	if *cpuprofile == "" {
		return nil
	}

	f, err := os.Create(*cpuprofile)
	if err != nil {
		return fmt.Errorf("failed to create CPU profile file: %w", err)
	}

	if err := pprof.StartCPUProfile(f); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			log.Warn("Error closing CPU profile file: %v", closeErr)
		}
		return fmt.Errorf("error starting CPU profile: %w", err)
	}

	cpuProfileFile = f
	log.Info("CPU profiling started, output: %s", *cpuprofile)
	return nil
}

func cleanupCPUProfile() {
	if cpuProfileFile != nil {
		pprof.StopCPUProfile()
		if err := cpuProfileFile.Close(); err != nil {
			log.Error("Error closing CPU profile file: %v", err)
		}
		log.Info("CPU profiling stopped")
	}
}

func runBenchmark(data <-chan *types.LiveChatMessage) BenchmarkResult {
	log.Info("Starting benchmark for %d seconds", *duration)

	startMem := getMemStats()
	startTime := time.Now()

	msgCount := collectMessages(data)

	endTime := time.Now()
	endMem := getMemStats()
	elapsed := endTime.Sub(startTime)

	return BenchmarkResult{
		Duration:     elapsed,
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
	log.Info("\n=== Benchmark Results ===")
	log.Info("Duration: %v", result.Duration)
	log.Info("Messages: %d", result.MessageCount)
	log.Info("Msg/sec: %.2f", float64(result.MessageCount)/result.Duration.Seconds())

	log.Info("\n=== Memory Usage ===")
	log.Info("Start: %.2f MB", result.StartMem.AllocMB)
	log.Info("End: %.2f MB", result.EndMem.AllocMB)
	log.Info("Diff: %.2f MB", result.EndMem.AllocMB-result.StartMem.AllocMB)
	log.Info("GC Runs: %d", result.EndMem.NumGC-result.StartMem.NumGC)
	log.Info("Goroutines: %d", result.GoRoutines)
}

func writeMemoryProfile() error {
	if *memprofile == "" {
		return nil
	}

	f, err := os.Create(*memprofile)
	if err != nil {
		return fmt.Errorf("failed to create memory profile file: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Error("Error closing memory profile file: %v", closeErr)
		}
	}()

	runtime.GC()
	if err := pprof.WriteHeapProfile(f); err != nil {
		return fmt.Errorf("failed to write heap profile: %w", err)
	}

	log.Info("Memory profile written to: %s", *memprofile)
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

