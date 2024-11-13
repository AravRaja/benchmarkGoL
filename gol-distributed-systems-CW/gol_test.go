package main

import (
	"fmt"
	"testing"

	"uk.ac.bris.cs/gameoflife/gol"
	"uk.ac.bris.cs/gameoflife/util"
)

// TestGol tests 16x16, 64x64 and 512x512 images on 0, 1 and 100 turns using 1-16 worker threads.
func TestGol(t *testing.T) {
	tests := []gol.Params{
		{ImageWidth: 16, ImageHeight: 16},
		{ImageWidth: 64, ImageHeight: 64},
		{ImageWidth: 512, ImageHeight: 512},
	}

	// Add a flag to track if a test failed
	testFailed := false

	for _, p := range tests {
		if testFailed {
			break
		}

		for _, turns := range []int{0, 1, 100} {
			if testFailed {
				break
			}

			p.Turns = turns
			expectedAlive := readAliveCells(
				"check/images/"+fmt.Sprintf("%vx%vx%v.pgm", p.ImageWidth, p.ImageHeight, turns),
				p.ImageWidth,
				p.ImageHeight,
			)

			for threads := 1; threads <= 16; threads++ {
				if testFailed {
					break
				}

				p.Threads = threads
				testName := fmt.Sprintf("%dx%dx%d-%d", p.ImageWidth, p.ImageHeight, p.Turns, p.Threads)
				t.Run(testName, func(t *testing.T) {
					events := make(chan gol.Event)
					go gol.Run(p, events, nil)
					var cells []util.Cell

					for event := range events {
						switch e := event.(type) {
						case gol.FinalTurnComplete:
							cells = e.Alive
						}
					}

					if !assertEqualBoard(t, cells, expectedAlive, p) {
						t.Errorf("Test failed, quitting immediately.")
						testFailed = true // Mark test as failed
					}
				})
			}
		}
	}

	if testFailed {
		t.FailNow()
	}
}
