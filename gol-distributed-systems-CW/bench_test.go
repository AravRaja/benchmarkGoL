// Example useage:
// go test -run a$ -bench BenchmarkStudentVersion/512x512x1000-1 -timeout 100s -cpuprofile cpu.prof
// to get results into .out file
// go test -run ^$ -bench BenchmarkStudentVersion -benchtime 1x -count 4 | tee serial_implementation.out
// to convert .out file to csv file
// go run golang.org/x/perf/cmd/benchstat -format csv results.out | tee results.csv
package main

import (
	"fmt"
	"os"
	"testing"

	"uk.ac.bris.cs/gameoflife/gol"
)

const benchLength = 100

var imageWidths = []int{128, 256, 512, 1024, 2048}

func BenchmarkStudentVersion(b *testing.B) {
	for _, width := range imageWidths {

		os.Stdout = nil // Disable all program output apart from benchmark results
		p := gol.Params{
			Turns:       benchLength,
			Threads:     1,
			ImageWidth:  width,
			ImageHeight: width,
		}
		name := fmt.Sprintf("%dx%dx%d-%d", p.ImageWidth, p.ImageHeight, p.Turns, p.Threads)
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				events := make(chan gol.Event)
				go gol.Run(p, events, nil)
				for range events {
				}
			}
		})

	}
}
