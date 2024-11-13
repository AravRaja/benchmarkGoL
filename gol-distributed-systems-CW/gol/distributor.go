package gol

import (
	"flag"
	"fmt"
	"net/rpc"
	"sync"
	"time"

	"uk.ac.bris.cs/gameoflife/gol/stubs"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
	keyPresses <-chan rune
}

var (
	client *rpc.Client
	once   sync.Once
)

func initClient() {
	server := flag.String("server", "172.31.40.188:8030", "IP:port string to connect to as server")
	flag.Parse()
	var err error
	client, err = rpc.Dial("tcp", *server)
	if err != nil {
		fmt.Println("Failed to connect to server so client is NOT initialised.", err)
	}
}

// dont want to init the client at each Run call so used once.Do so only happens first time
func getClient() *rpc.Client {
	once.Do(initClient)
	return client
}

// make a blocking rpc call to broker to get the final updated world
// if quit signal is received, return the input world
func runNodes(p Params, world [][]byte, c distributorChannels, quit <-chan struct{}) [][]byte {
	c.events <- StateChange{0, Executing}
	request := &stubs.InitRunRequest{World: world, Width: p.ImageWidth, Height: p.ImageHeight, Turns: p.Turns}
	response := new(stubs.RunResponse)
	resultChan := make(chan error, 1)

	go func() {
		resultChan <- getClient().Call(stubs.RunNodesHandler, request, response)
	}()

	select {
	case err := <-resultChan:
		if err != nil {
			fmt.Println("Error when getting update: could not update World, returning input World!", err)
			return world
		}

		c.events <- FinalTurnComplete{p.Turns, response.AliveCells}

		return response.World

	case <-quit:

		return world

	}
}

// use a ticker to query the broker every 2 seconds for the number of alive cells
func reportAliveCells(c distributorChannels, quit <-chan struct{}) {

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			request := &stubs.EmptyRequest{}
			response := new(stubs.AliveCellsResponse)
			err := getClient().Call(stubs.GetAliveCellsHandler, request, response)
			if err != nil {
				fmt.Println("Error when getting alive cell count from client.", err)
				continue
			}

			c.events <- AliveCellsCount{response.Turn, response.AliveCells}

		case <-quit:

			return
		}
	}
}

// rpc call to broker to get World
func getWorld() ([][]byte, int) {
	request := &stubs.EmptyRequest{}
	response := new(stubs.WorldResponse)
	err := getClient().Call(stubs.GetWorldHandler, request, response)
	if err != nil {
		fmt.Println("Error when getting World from client.", err)
		return nil, 0
	}
	return response.World, response.Turn
}

// reads world from ioInput
func readWorld(p Params, c distributorChannels) [][]byte {
	c.ioCommand <- ioInput
	w := fmt.Sprintf("%d", p.ImageWidth)
	h := fmt.Sprintf("%d", p.ImageHeight)
	filename := w + "x" + h
	c.ioFilename <- filename
	world := make([][]byte, p.ImageHeight)

	for i := range world {
		world[i] = make([]byte, p.ImageWidth)
	}
	for j := range world {
		for k := range world[j] {
			aod := <-c.ioInput
			world[j][k] = aod
		}
	}
	return world
}

// writes world to ioOutput and sends ImageOutputComplete event
func writeWorld(c distributorChannels, world [][]byte, turn int) {
	c.ioCommand <- ioOutput // tell io command we want to write a pgm image
	w := fmt.Sprintf("%d", len(world[0]))
	h := fmt.Sprintf("%d", len(world))
	t := fmt.Sprintf("%d", turn)

	filename := w + "x" + h + "x" + t // construct filename and send to io
	c.ioFilename <- filename

	for a := range world { // send byte by byte to output
		for b := range world[a] {
			c.ioOutput <- world[a][b]
		}
	}
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	c.events <- ImageOutputComplete{turn, filename}
}

// rpc call to broker to notify of client quitting
func notifyServerOfQuit() {
	emptyRequest := &stubs.EmptyRequest{}

	response := new(stubs.QuitResponse)

	err := getClient().Call(stubs.OnClientQuitHandler, emptyRequest, response)
	if err != nil {
		fmt.Println("Error in server registering client quitting.", err)

	}
}

// rpc call to broker telling it to shutdown the system completely
func shutdownServer() {
	emptyRequest := &stubs.EmptyRequest{}

	response := new(stubs.EmptyResponse)

	err := getClient().Call(stubs.ShutdownHandler, emptyRequest, response)
	if err != nil {
		fmt.Println("Error in server shutting down.", err)

	}
}

// gracefully closes c.events before exiting the program
func quitChannels(c distributorChannels, turn int) {

	c.ioCommand <- ioCheckIdle // Ensure that IO has finished any output before exiting.
	<-c.ioIdle
	c.events <- StateChange{turn, Quitting}
	close(c.events) // Close the channel to stop the SDL goroutine gracefully.

}

// function that runs the game of life takes in all the parameters and channels and designates accoridng
func distributor(p Params, c distributorChannels) {
	world := readWorld(p, c)
	if world == nil {
		fmt.Println("World could not be read! EXITING")
		return
	}

	quit := make(chan struct{})
	systemShutdown := make(chan struct{})
	paused := false
	pauseCond := sync.NewCond(&sync.Mutex{})
	go reportAliveCells(c, quit)
	go func() { //anonymous function to handle keypresses
		emptyRequest := &stubs.EmptyRequest{}
		quitClosed := false
		systemShutdownClosed := false

		for key := range c.keyPresses {
			switch key {
			case 's':
				world, turn := getWorld()
				writeWorld(c, world, turn)
			case 'p':
				pauseCond.L.Lock()
				paused = !paused
				pauseCond.L.Unlock()
				pauseCond.Broadcast()
				response := new(stubs.PauseResponse)

				err := getClient().Call(stubs.TogglePauseHandler, emptyRequest, response)
				if err != nil {
					fmt.Println("Error when trying to Pause server.", err)
					continue
				}

				if paused {
					c.events <- StateChange{response.Turn, Paused}

				} else {
					c.events <- StateChange{response.Turn, Executing}

				}

			case 'q':

				if !quitClosed {
					close(quit)
					quitClosed = true
				}
			case 'k':

				if !quitClosed {
					close(quit)
					quitClosed = true
				}
				if !systemShutdownClosed {
					close(systemShutdown)
					systemShutdownClosed = true
				}
			}
		}
	}()
	world = runNodes(p, world, c, quit)

	select {
	case <-quit:
	default:
		for paused { //doesnt quit immeditely if paused waits until unpaused

			pauseCond.L.Lock()
			pauseCond.Wait()
			pauseCond.L.Unlock()
		}
		close(quit)

	}

	notifyServerOfQuit()

	writeWorld(c, world, p.Turns)
	quitChannels(c, p.Turns)

	select {
	case <-systemShutdown:
		shutdownServer()
	default:
		close(systemShutdown)
	}

}
