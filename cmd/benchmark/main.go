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
)

var (
	duration   = flag.Int("duration", 60, "benchmark duration in seconds")
	tracefile  = flag.String("trace", "", "write trace to file")
	memprofile = flag.String("memprofile", "", "write memory profile to file")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
)

func main() {
	flag.Parse()

	if *tracefile != "" {
		f, err := os.Create(*tracefile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		if err := trace.Start(f); err != nil {
			log.Fatal(err)
		}
		defer trace.Stop()
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: benchmark [options] <channel_url>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	channelURL := flag.Arg(0)

	log.Printf("Starting benchmark for %d seconds", *duration)
	startMem := getMemStats()
	startTime := time.Now()

	chat, err := scrapchat.New("youtube", false)
	if err != nil {
		log.Fatalf("Failed to create scraper: %v", err)
	}

	data, err := chat.FetchLiveChat(channelURL)
	if err != nil {
		log.Fatalf("Error fetching live chat: %v", err)
	}

	msgCount := 0
	timeout := time.After(time.Duration(*duration) * time.Second)
	memSamples := []memStat{}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

loop:
	for {
		select {
		case msg, ok := <-data:
			if !ok {
				break loop
			}
			msgCount++
			_ = msg
		case <-ticker.C:
			memSamples = append(memSamples, getMemStats())
		case <-timeout:
			break loop
		}
	}

	endTime := time.Now()
	endMem := getMemStats()
	duration := endTime.Sub(startTime)

	log.Printf("\n=== Benchmark Results ===")
	log.Printf("Duration: %v", duration)
	log.Printf("Messages: %d", msgCount)
	log.Printf("Msg/sec: %.2f", float64(msgCount)/duration.Seconds())
	log.Printf("\n=== Memory Usage ===")
	log.Printf("Start: %.2f MB", startMem.AllocMB)
	log.Printf("End: %.2f MB", endMem.AllocMB)
	log.Printf("Peak: %.2f MB", getPeakMemory(memSamples))
	log.Printf("Average: %.2f MB", getAvgMemory(memSamples))
	log.Printf("Diff: %.2f MB", endMem.AllocMB-startMem.AllocMB)
	log.Printf("GC Runs: %d", endMem.NumGC-startMem.NumGC)
	log.Printf("Goroutines: %d", runtime.NumGoroutine())

	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Fatal(err)
		}
	}
}

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

func getPeakMemory(samples []memStat) float64 {
	peak := 0.0
	for _, s := range samples {
		if s.AllocMB > peak {
			peak = s.AllocMB
		}
	}
	return peak
}

func getAvgMemory(samples []memStat) float64 {
	if len(samples) == 0 {
		return 0
	}
	sum := 0.0
	for _, s := range samples {
		sum += s.AllocMB
	}
	return sum / float64(len(samples))
}
