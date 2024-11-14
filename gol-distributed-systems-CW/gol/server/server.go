package main

import (
	"flag"
	"fmt"
	"math/rand"
	"net"
	"net/rpc"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"uk.ac.bris.cs/gameoflife/gol/stubs"
	"uk.ac.bris.cs/gameoflife/util"
)

type WorldInfo struct { //struct used for buffer so each past turn we stort the world and alive cells
	world      [][]byte
	aliveCells int
}

// calculates and returns the coordinates of all alive cells in the util.Cell format
func calculateAliveCells(w int, h int, world [][]byte) []util.Cell {
	var cells []util.Cell
	for i := 0; i < h; i++ {
		for j := 0; j < w; j++ {
			if world[i][j] == 0xff {
				cells = append(cells, util.Cell{X: j, Y: i})
			}
		}
	}
	return cells
}

// calculates the next state of the world with the halo regions
func calculateNextState(w int, h int, world [][]byte, aboveBorder []byte, belowBorder []byte) ([][]byte, int) {
	world = append([][]byte{aboveBorder}, world...)
	world = append(world, belowBorder)

	aliveCells := 0

	newWorld := make([][]byte, h)
	for b := range newWorld {
		newWorld[b] = make([]byte, w)
	}
	var aNo byte
	for i := 1; i <= h; i++ {
		for j := 0; j < w; j++ {

			aNo = 0
			if j == 0 {

				aNo = world[i-1][j]/0xFF + world[i-1][j+1]/0xFF + world[i+1][j]/0xFF + world[i+1][j+1]/0xFF + world[i][j+1]/0xFF + world[i][w-1]/0xFF + world[i+1][w-1]/0xFF + world[i-1][w-1]/0xFF
				//instead of looping through all the cells we can jus query the exact cells next to thus slightly more efficient
			} else if j == w-1 {
				aNo = world[i-1][j]/0xFF + world[i-1][j-1]/0xFF + world[i+1][j]/0xFF + world[i+1][j-1]/0xFF + world[i][j-1]/0xFF + world[i][0]/0xFF + world[i+1][0]/0xFF + world[i-1][0]/0xFF
			} else {
				aNo = world[i-1][j]/0xFF + world[i+1][j]/0xFF + world[i][j-1]/0xFF + world[i][j+1]/0xFF + world[i+1][j-1]/0xFF + world[i+1][j+1]/0xFF + world[i-1][j-1]/0xFF + world[i-1][j+1]/0xFF
			}

			if world[i][j] == 0xFF {
				if aNo < 2 || aNo > 3 {
					newWorld[i-1][j] = 0x00
				} else {
					newWorld[i-1][j] = 0xFF
					aliveCells++
				}
			} else {
				if aNo == 3 {

					newWorld[i-1][j] = 0xFF
					aliveCells++
				} else {
					newWorld[i-1][j] = 0x00
				}
			}
		}
	}

	return newWorld, aliveCells
}

// rpc call to node underneath it giving halo regions, stop signals if plausible and a value safe to Delete from buffer
func haloExchange(world [][]byte, safeDelete int, turn int, belowNode *rpc.Client, initStopRun bool) ([]byte, string) {

	req := &stubs.BorderRequest{
		Border:     world[len(world)-1],
		SafeDelete: safeDelete,
		Turn:       turn,
		StopRun:    initStopRun,
	}
	res := new(stubs.BorderResponse)
	err := belowNode.Call(stubs.GetBorderHandler, req, res)
	if err != nil {
		fmt.Println("Error sending border to belowNode:", err)
		return nil, ""
	}

	return res.Border, res.Status
}

// runs when the RunNode function completes sends the info back to the broker with the nodeID so the broker can put the whole image together when all nodes completed
func completed(world [][]byte, nodeID int, brokerAddress string) {

	client, err := rpc.Dial("tcp", brokerAddress)
	if err != nil {
		fmt.Println("Error dialing broker:", err)
		return
	}
	defer client.Close()

	req := &stubs.NodeCompletionRequest{
		World:      world,
		AliveCells: (calculateAliveCells(len(world[0]), len(world), world)),
		NodeID:     nodeID,
	}
	res := new(stubs.EmptyResponse)
	err = client.Call(stubs.OnNodeCompletionHandler, req, res)
	if err != nil {
		fmt.Println("Error calling OnNodeCompletion:", err)
		return
	}
}

type NodeOperations struct {
	brokerAddress     string
	world             [][]byte
	aliveCells        int
	initStopRun       bool
	nodeID            int
	neighbourNodeStop bool
	worldBuffer       map[int]WorldInfo //our way of handling synchronisation
	width             int
	height            int
	leader            bool
	turn              int
	turns             int
	turnSync          int
	isPaused          bool
	stopUpdate        bool
	safeDelete        int
	belowNode         *rpc.Client
	aboveBorder       []byte
	belowBorder       []byte
	nodeNum           int
	mu                sync.RWMutex
	listener          net.Listener
	pauseCond         *sync.Cond
	haloCond          *sync.Cond
	running           bool
}

// NewNodeOperations initializes and returns a new NodeOperations instance.
func NewNodeOperations() *NodeOperations {
	n := &NodeOperations{
		isPaused:      false,
		worldBuffer:   make(map[int]WorldInfo),
		brokerAddress: "172.31.40.188:8030",
	}
	n.pauseCond = sync.NewCond(&n.mu)
	n.haloCond = sync.NewCond(&n.mu)
	return n
}

// Gets called after all the nodes are registered initialised node neighbours as well as other key info
// dials nodes into belowNode as well
func (n *NodeOperations) Init(req *stubs.InitNodeRequest, res *stubs.InitNodeResponse) (err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.aboveBorder = req.AboveBorder
	n.belowBorder = req.BelowBorder
	n.nodeNum = req.NodeNum
	n.leader = req.Leader
	n.nodeID = req.NodeID

	if req.BelowNode != "" {

		n.belowNode, err = rpc.Dial("tcp", req.BelowNode)
		if err != nil {
			fmt.Println("Error connecting to belowNode:", err)
			return err
		}
	}

	fmt.Println("Node initialized with neighbors")
	return nil
}

// Function only for leader node to tell the system to stop deleting from buffer as a result is being queried eg alive cells/getWorld
func (n *NodeOperations) SafeModeOn(req *stubs.EmptyRequest, res *stubs.TurnResponse) (err error) {

	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.leader {
		return nil
	}

	n.safeDelete = -1
	if n.turn-n.nodeNum < 0 {
		res.Turn = 0
	} else {
		res.Turn = n.turn - (n.nodeNum + 1)
	}

	return nil
}

// Leader only function, turns off SafeMode so the buffer starts to delete like normal
// Normally Ran after data query in broker e.g after GetWorld has got the world from buffer from all nodes
func (n *NodeOperations) SafeModeOff(req *stubs.EmptyRequest, res *stubs.EmptyResponse) (err error) {

	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.leader {

		return nil
	}
	n.safeDelete = n.turn - (n.nodeNum + 1)

	return nil
}

// takes an input of turn and returns the associated world from buffer
func (n *NodeOperations) GetWorld(req *stubs.InfoRequest, res *stubs.NodeWorldResponse) (err error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	res.World = n.worldBuffer[req.Turn].world
	return nil
}

// takes an input of turn and returns the associated AliveCells from buffer
func (n *NodeOperations) GetAliveCells(req *stubs.InfoRequest, res *stubs.AliveCellsResponse) (err error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	res.AliveCells = n.worldBuffer[req.Turn].aliveCells
	return nil
}

// Leader Function agaian but broadcasts a pause Variable which blocks the leader in the mian runNode loop this in turn blocks all the other nodes
func (n *NodeOperations) TogglePause(req *stubs.EmptyRequest, res *stubs.PauseResponse) (err error) {

	n.mu.Lock()
	defer n.mu.Unlock()

	n.isPaused = !n.isPaused

	if !n.isPaused {
		n.pauseCond.Broadcast()
	}
	res.IsPaused = n.isPaused

	return nil
}

// Run Values that have to be iniated for all the nodes before the actual running of any single node can take place
func (n *NodeOperations) InitRun(req *stubs.InitRunRequest, res *stubs.StatusReport) (err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.turn = 0
	n.stopUpdate = false
	n.isPaused = false
	n.world = req.World
	n.neighbourNodeStop = false
	n.width = req.Width
	n.height = req.Height
	n.turnSync = 0
	n.safeDelete = -1
	n.aboveBorder = req.AboveBorder
	n.belowBorder = req.BelowBorder
	n.aliveCells = len(calculateAliveCells(n.width, n.height, n.world))
	n.initStopRun = false
	n.stopUpdate = false
	n.turns = req.Turns
	res.Message = "Node ready to be Run"
	return
}

// Leader Function Stops the running of the nodes by sending in two signals via halo exchange the first from the leader
// propagates the initStop signal to all the nodes to all safe stopping without deadlock
// and the second signal is the actual stop signal
func (n *NodeOperations) OnClientQuit(req *stubs.EmptyRequest, res *stubs.QuitResponse) (err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.leader {
		return fmt.Errorf("error: not leader being called")
	}
	if !n.running {

		return
	}

	n.initStopRun = true
	n.haloCond.Broadcast()
	for !n.stopUpdate {
		n.haloCond.Wait()
	}
	return nil
}

// function that actual runs nodes and calculates the next state of the world
func (n *NodeOperations) RunNode(req *stubs.EmptyRequest, res *stubs.EmptyResponse) (err error) {
	fmt.Println("RunNode started")
	n.mu.Lock()
	n.running = true
	n.mu.Unlock()
	for i := 1; i <= n.turns; i++ {
		n.mu.Lock()

		if n.safeDelete != -1 && n.leader && n.turn-(n.nodeNum+1) > n.safeDelete {
			n.safeDelete = n.turn - (n.nodeNum + 1)

		} //If Leader need to increase safeDelete as turn increases so the buffer stays a manageable size

		for n.isPaused {

			n.pauseCond.Wait()
		} //When Is Paused RunNode is waiting which will pause all the nodes as they all require the leader in some form to finsih albeit indirectly

		n.worldBuffer[n.turn] = WorldInfo{world: n.world, aliveCells: n.aliveCells}
		n.world, n.aliveCells = calculateNextState(n.width, n.height, n.world, n.aboveBorder, n.belowBorder)
		//stores Buffer and Calculates Nexts state

		n.turn++
		n.haloCond.Broadcast()

		if n.initStopRun {

			//when the initStopRun signal stop calculation and get into a safer haloMode where your jus waiting for one more signal
			//from your previous node beofre stopping
			status := ""
			for {
				n.belowBorder, status = haloExchange(n.world, n.safeDelete, n.turn, n.belowNode, n.initStopRun)

				if status == "Stopped" {
					fmt.Println("STOPPED: StopRun signal received, exiting")
					n.stopUpdate = true
					n.mu.Unlock()
					return
				}
				for !n.neighbourNodeStop {

					n.haloCond.Wait()
				}
			}

		}

		haloChan := make(chan []byte)
		go func() {
			border, _ := haloExchange(n.world, n.safeDelete, n.turn, n.belowNode, n.initStopRun)
			haloChan <- border
		}()
		n.mu.Unlock()
		belowBorder := <-haloChan //done so everything can be locked correctly without any big wait times
		n.mu.Lock()
		n.belowBorder = belowBorder

		for n.turnSync != n.turn && !n.initStopRun {

			n.haloCond.Wait()
		} //makes sure all nodes are in sync before proceeding to the next turn

		n.mu.Unlock()
	}
	n.mu.Lock()
	n.running = false
	n.mu.Unlock()
	fmt.Println("All turns completed, finalizing")
	completed(n.world, n.nodeID, n.brokerAddress)

	return nil
}

// kills the process and shutsdown
func (n *NodeOperations) Shutdown(req *stubs.EmptyRequest, res *stubs.EmptyResponse) (err error) {
	fmt.Println("Shutting down gracefully")
	n.mu.Lock()
	defer n.mu.Unlock()
	pid := os.Getpid()
	_ = syscall.Kill(pid, syscall.SIGINT)
	time.Sleep(1 * time.Second)
	return nil
}

// RPC Function from HaloExchange from aboveNode
// gets the border from the aboveNode and sends the associated border.
// if quit signal has been sent and in initStopRun Mode status is "stopping"
// otherwise status is running
func (n *NodeOperations) GetBorder(req *stubs.BorderRequest, res *stubs.BorderResponse) (err error) {

	n.mu.Lock()
	defer n.mu.Unlock()
	defaultBorder := make([]byte, n.width)
	if n.initStopRun && req.StopRun {
		res.Border = defaultBorder
		res.Status = "Stopped"
		n.neighbourNodeStop = true

		n.haloCond.Broadcast()
		return
	}
	if req.StopRun {
		n.initStopRun = true
		res.Border = defaultBorder
		res.Status = "Running"

		n.haloCond.Broadcast()
		return
	}
	// Wait until n.turn matches req.Turn
	for n.turn != req.Turn && !n.initStopRun {
		n.haloCond.Wait() // Wait until notified that n.turn may have changed
	}
	if n.initStopRun {

		res.Border = defaultBorder
		res.Status = "Stopped"

		n.haloCond.Broadcast()
		return
	}

	// Once the condition is met, update the values
	n.safeDelete = req.SafeDelete

	// Delete outdated entries from worldBuffer
	for turn := range n.worldBuffer {
		if turn < n.safeDelete {
			delete(n.worldBuffer, turn)
		}
	}

	n.aboveBorder = req.Border

	n.turnSync = req.Turn
	n.haloCond.Broadcast()

	// Set the response
	res.Border = n.world[0]
	res.Status = "Running"

	return nil
}

// run at beginning registers node with broker
func register(brokerAddr string, port string) string {
	client, err := rpc.Dial("tcp", brokerAddr)
	if err != nil {
		fmt.Println("Error dialing broker:", err)
		return ""
	}
	defer client.Close()

	req := &stubs.RegisterRequest{
		NodeAddress: getOutboundIP() + ":" + port,
	}
	res := new(stubs.StatusReport)
	err = client.Call(stubs.RegisterNodeHandler, req, res)
	if err != nil {
		fmt.Println("Error registering node with broker:", err)
	}
	return res.Message
}

// func from lab that gives outbound ip
func getOutboundIP() string {
	conn, _ := net.Dial("udp", "8.8.8.8:80")
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr).IP.String()
	return localAddr
}

// function I found ONLINE that allows for my shutdown functionality
func waitForShutdown() {

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	fmt.Println("Node is running. Press Ctrl+C to stop.")
	<-sigs
	fmt.Println("Shutdown signal received. Exiting...")
}

func main() {
	var port string
	if len(os.Args) > 1 {
		port = os.Args[1]
	} else {
		port = "8030"
	} //allows u to enter the port in e.g go run server.go 8070

	pAddr := flag.String("port", port, "Port to listen on")
	brokerAddr := "172.31.40.188:8030"
	flag.Parse()
	rand.Seed(time.Now().UnixNano())
	node := NewNodeOperations()
	err := rpc.Register(node)
	if err != nil {
		fmt.Println("Error registering RPC server:", err)
		return
	}

	listener, err := net.Listen("tcp", ":"+*pAddr)
	if err != nil {
		fmt.Println("Error starting TCP listener:", err)
		return
	}

	node.listener = listener
	defer listener.Close()

	fmt.Println("NodeOperations listening on port:", *pAddr)
	go rpc.Accept(listener) //runs this asycn as have to register to broker straight after
	register(brokerAddr, *pAddr)
	waitForShutdown()
}
