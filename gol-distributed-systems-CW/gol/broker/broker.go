package main

import (
	"flag"
	"fmt"
	"net"
	"net/rpc"

	"sync"

	"uk.ac.bris.cs/gameoflife/gol/stubs"
	"uk.ac.bris.cs/gameoflife/util"
)

type Node struct { // Node struct to store the rpc client and address
	client  *rpc.Client
	address string
}

type World struct { // World struct to store the board, width and height
	board  [][]byte
	width  int
	height int
}

func getOutboundIP() string {
	conn, _ := net.Dial("udp", "8.8.8.8:80")
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr).IP.String()
	return localAddr
}

// function to split world into subWorld depening on number of node registered
func splitWorldIntoSubWorlds(world World, numNodes int) []World {

	chunks := make([]int, numNodes)
	min := world.height / numNodes
	extra := world.height % numNodes
	for j := 0; j < numNodes; j++ {
		chunks[j] = int(min)
	}
	for k := 0; k < extra; k++ {
		chunks[k]++
	}
	yStart := 0
	var subWorlds []World
	for i := 0; i < numNodes; i++ {
		height := chunks[i]
		board := make([][]byte, height)
		for j := 0; j < height; j++ {
			board[j] = make([]byte, world.width)
			copy(board[j], world.board[yStart+j])
		}
		subWorlds = append(subWorlds, World{
			board:  board,
			width:  world.width,
			height: height,
		})
		yStart += height
	}
	return subWorlds
}
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
func initRun(nodes []*Node, world World, turns int) {

	nodeNum := len(nodes)
	worlds := splitWorldIntoSubWorlds(world, nodeNum)

	var wg sync.WaitGroup
	statusChan := make(chan *stubs.StatusReport, nodeNum)

	// Initialize each node in a goroutine
	for i := 0; i < nodeNum; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			var aboveBorder []byte
			var belowBorder []byte
			// Set the borders for each node
			if i == 0 {
				aboveBorder = worlds[nodeNum-1].board[len(worlds[nodeNum-1].board)-1]
				belowBorder = worlds[i+1].board[0]
			} else if i == nodeNum-1 {
				aboveBorder = worlds[i-1].board[len(worlds[i-1].board)-1]
				belowBorder = worlds[0].board[0]
			} else {
				aboveBorder = worlds[i-1].board[len(worlds[i-1].board)-1]
				belowBorder = worlds[i+1].board[0]
			}

			// Prepare the init request
			req := &stubs.InitRunRequest{
				AboveBorder: aboveBorder,
				BelowBorder: belowBorder,
				World:       worlds[i].board,
				Width:       worlds[i].width,
				Height:      worlds[i].height,
				Turns:       turns,
			}
			res := new(stubs.StatusReport)

			// Call InitRunHandler and send the result to the channel
			err := nodes[i].client.Call(stubs.InitRunHandler, req, res)
			if err != nil {
				fmt.Printf("Error calling init run on node %d: %v\n", i, err)
			} else {
				fmt.Printf("Node %d: InitRunHandler called successfully\n", i)
				statusChan <- res
			}
		}(i)
	}

	wg.Wait()
	close(statusChan)

	// Wait for all nodes to send their status report
	for status := range statusChan {
		fmt.Printf("Received status from node: %v\n", status.Message)
	}
	//this is important as all nodes need to have all the initRun data before any of the nodes can be run

	// Call RunNodeHandler on each node in sequence

	for i := 0; i < nodeNum; i++ {
		req := new(stubs.EmptyRequest)
		res := new(stubs.EmptyResponse)

		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			err := nodes[i].client.Call(stubs.RunNodeHandler, req, res)
			if err != nil {
				fmt.Printf("Error calling run node on node %d: %v\n", i, err)
			} else {
				fmt.Printf("RunNode Finished %d\n", i)
			}
		}(i)
	}

	wg.Wait()
	return
}

// run after all nodes registered assiginging a leader node and giving each node the address to below node
func initNodes(nodes []*Node) {

	nodeNum := len(nodes)

	var belowNode string

	for i := 0; i < nodeNum; i++ {

		belowNode = nodes[(i+1)%nodeNum].address

		req := &stubs.InitNodeRequest{
			BelowNode: belowNode,
			Leader:    i == 0,
			NodeNum:   nodeNum,
			NodeID:    i,
		}
		res := &stubs.InitNodeResponse{}
		err := nodes[i].client.Call(stubs.InitNodeHandler, req, res)
		if err != nil {
			fmt.Printf("Error calling init on node %d: %v\n", i, err)
		}
	}
	fmt.Println("Nodes initialized successfully")
}

// Rpc calls safemode to get a turn thats safe to query
func safeModeOn(leader *rpc.Client) (num int) {

	req := &stubs.EmptyRequest{}
	res := &stubs.TurnResponse{}

	err := leader.Call(stubs.SafeModeOnHandler, req, res)

	if err != nil {
		fmt.Printf("Error calling leader %v\n", err)
		return
	}

	return res.Turn
}

// Turns off SafeMode so buffer can start deleting again
func safeModeOff(leader *rpc.Client) {
	req := &stubs.EmptyRequest{}
	res := &stubs.EmptyResponse{}
	err := leader.Call(stubs.SafeModeOffHandler, req, res)
	if err != nil {
		fmt.Printf("Error calling leader %v\n", err)
	}
}

type BrokerOperations struct {
	world               World
	turn                int
	finishedRendering   bool
	turns               int
	isPaused            bool
	stopUpdate          bool
	mu                  sync.RWMutex
	cond                *sync.Cond
	listener            net.Listener
	nodes               []*Node
	nodesCompleted      map[int][][]byte
	aliveCellsCompleted []util.Cell
	Leader              *Node
	expectedNodes       int
}

func NewBrokerOperations() *BrokerOperations {
	b := &BrokerOperations{
		isPaused: false,
	}
	b.cond = sync.NewCond(&b.mu)

	return b
}

// Node calls this function on boot to register with broker
func (b *BrokerOperations) RegisterNode(req *stubs.RegisterRequest, res *stubs.StatusReport) (err error) {
	client, errTwo := rpc.Dial("tcp", req.NodeAddress)
	if errTwo != nil {
		return fmt.Errorf("failed to connect to node: %w", errTwo)
	}

	node := &Node{
		client:  client,
		address: req.NodeAddress,
	}
	b.mu.Lock()
	b.nodes = append(b.nodes, node)
	fmt.Println("Node registered successfully at", req.NodeAddress)
	b.cond.Signal() // Notify waiting goroutine
	b.mu.Unlock()
	res.Message = "Node registered successfully"
	return nil
}

// RunNodes is called by client to start the world calculations, broker splits up world and sends to diff nodes
// will only terminate if finalRender is ture meaning all nodes have returned a final world
// or if stopUpdate is True to force return for when client quits
func (b *BrokerOperations) RunNodes(req *stubs.InitRunRequest, res *stubs.RunResponse) (err error) {
	fmt.Println("Started RunNodes")
	b.mu.Lock()
	world := World{
		board:  req.World,
		width:  req.Width,
		height: req.Height,
	}

	b.stopUpdate = false
	b.finishedRendering = false
	b.world = world

	b.turn = 0
	b.turns = req.Turns

	b.nodesCompleted = make(map[int][][]byte)
	b.aliveCellsCompleted = nil
	b.stopUpdate = false
	b.isPaused = false
	b.stopUpdate = false
	b.isPaused = false

	go initRun(b.nodes, world, req.Turns)
	for !b.stopUpdate && !b.finishedRendering {
		b.cond.Wait()
	}
	for b.isPaused {
		b.cond.Wait()
	}
	if b.finishedRendering {
		b.turn = req.Turns
	}

	res.World = b.world.board
	res.Turn = b.turn
	res.AliveCells = b.aliveCellsCompleted
	b.mu.Unlock()

	return
}

// Node calls this function when it has finished rendering the world
func (b *BrokerOperations) OnNodeCompletion(req *stubs.NodeCompletionRequest, res *stubs.EmptyResponse) (err error) {

	b.mu.Lock()
	defer b.mu.Unlock()

	b.nodesCompleted[req.NodeID] = req.World

	// Sort the nodesCompleted by NodeID

	if len(b.nodesCompleted) == len(b.nodes) {
		// All nodes have completed, update the world and notify RunNodes to respond to the client
		var newWorld [][]byte
		for i := 0; i < len(b.nodes); i++ {
			newWorld = append(newWorld, b.nodesCompleted[i]...)
		}
		b.world.board = newWorld
		b.aliveCellsCompleted = calculateAliveCells(b.world.width, b.world.height, b.world.board)
		b.finishedRendering = true

		b.cond.Broadcast()
	}

	return nil
}

// Called by client every two secs to get alive cells
func (b *BrokerOperations) GetAliveCells(req *stubs.EmptyRequest, res *stubs.AliveCellsResponse) (err error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	turn := safeModeOn(b.Leader.client) // get a safe turn state to query and stop deletion of buffer
	if err != nil {
		return err
	}
	b.turn = turn

	aliveCells := 0
	for _, node := range b.nodes { // query the said turn state from all nodes
		req := &stubs.InfoRequest{Turn: turn}
		res := new(stubs.AliveCellsResponse)
		err := node.client.Call(stubs.NodeGetAliveCellsHandler, req, res)
		if err != nil {
			fmt.Printf("Error calling GetAliveCells on node %s: %v\n", node.address, err)
			continue
		}
		aliveCells += res.AliveCells
	}
	safeModeOff(b.Leader.client) //turn off safeMode so deleetion in buffer can restart

	res.AliveCells = aliveCells

	//res.AliveCellsNumber = len(calculateAliveCells(b.width, b.height, b.world))
	res.Turn = b.turn
	return
}

// Same as GetAliveCells Function but for Get World
// Gets a safe turn and query all nodes for world from buffer and returns
func (b *BrokerOperations) GetWorld(req *stubs.EmptyRequest, res *stubs.WorldResponse) (err error) {

	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.finishedRendering {

		res.World = b.world.board
		res.Turn = b.turn
		return
	}

	turn := safeModeOn(b.Leader.client)
	if err != nil {

		return err
	}
	b.turn = turn

	var newWorld [][]byte
	for _, node := range b.nodes {
		fmt.Println("Requesting world from node:", node.address)
		req := &stubs.InfoRequest{Turn: turn}
		nodeRes := new(stubs.WorldResponse)
		err := node.client.Call(stubs.NodeGetWorldHandler, req, nodeRes)
		if err != nil {
			fmt.Printf("Error calling GetWorld on node %s: %v\n", node.address, err)
			continue
		}
		fmt.Println("Received world from node:", node.address)
		newWorld = append(newWorld, nodeRes.World...)
	}

	safeModeOff(b.Leader.client)

	b.world.board = newWorld

	res.World = b.world.board
	res.Turn = b.turn

	return
}

// When Pause signal is sent from client only the leader is actually called to pause
// This is due to the inbuilt sychro the halos have that they can only be at max numNodes/2 out of sync
// Thus when leader is paused all nodes will be "paused" waiting for halo until leader is unpaused
func (b *BrokerOperations) TogglePause(req *stubs.EmptyRequest, res *stubs.PauseResponse) (err error) {

	b.mu.Lock()
	defer b.mu.Unlock()

	b.isPaused = !b.isPaused
	b.cond.Broadcast()
	reqLeader := &stubs.EmptyRequest{}
	resLeader := &stubs.EmptyResponse{}
	err = b.Leader.client.Call(stubs.NodeTogglePauseHandler, reqLeader, resLeader)
	turn := safeModeOn(b.Leader.client)
	safeModeOff(b.Leader.client)
	b.turn = turn
	if err != nil {
		fmt.Printf("Error calling pause on leader: %v\n", err)
	}
	if !b.isPaused {
		b.cond.Broadcast()
	}
	res.IsPaused = b.isPaused
	res.Turn = turn

	return
}

// Sends the InitStopRun signal to leader node
// see server.go's doc for more ingo
func (b *BrokerOperations) OnClientQuit(req *stubs.EmptyRequest, res *stubs.QuitResponse) (err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopUpdate = true
	b.cond.Broadcast()
	reqLeader := &stubs.EmptyRequest{}
	resLeader := &stubs.EmptyResponse{}

	if b.finishedRendering {

		return
	}
	if b.isPaused {
		reqLeader := &stubs.EmptyRequest{}
		resLeader := &stubs.EmptyResponse{}
		err := b.Leader.client.Call(stubs.NodeTogglePauseHandler, reqLeader, resLeader)
		if err != nil {
			fmt.Printf("Error calling pause on leader: %v\n", err)
		}
		b.isPaused = false
	}

	fmt.Println("Nodes are starting to quit")
	errLeader := b.Leader.client.Call(stubs.NodeOnClientQuitHandler, reqLeader, resLeader)
	if errLeader != nil {
		fmt.Printf("Error calling OnClientQuit on leader: %v\n", err)
	}
	fmt.Println("Nodes have quit")
	res.Turn = b.turn

	return
}

// first gets all nodes in a safe positon to shutdown by terminating all functions like run
// then loop through nodes and shut them down
// then shuts oneself down
func (b *BrokerOperations) Shutdown(req *stubs.EmptyRequest, res *stubs.EmptyResponse) (err error) {

	reqLeader := &stubs.EmptyRequest{}
	resLeader := &stubs.EmptyResponse{}
	err = b.Leader.client.Call(stubs.NodeOnClientQuitHandler, reqLeader, resLeader)
	if err != nil {
		fmt.Printf("Error calling OnClientQuit on leader: %v\n", err)
	}

	for _, node := range b.nodes {
		req := &stubs.EmptyRequest{}
		res := &stubs.EmptyResponse{}
		err := node.client.Call(stubs.NodeShutdownHandler, req, res)
		if err != nil {
			fmt.Printf("Error calling Shutdown on node %s: %v\n", node.address, err)
		}
	}
	fmt.Println("Shutting down gracefully")
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listener != nil {
		err := b.listener.Close()
		if err != nil {
			fmt.Println("Error closing listener:", err)
			return err
		}
	}
	return
}

func main() {
	fmt.Println("Address:", getOutboundIP())
	pAddr := flag.String("port", "8030", "Port to listen on")
	flag.Parse()
	broker := NewBrokerOperations()
	broker.expectedNodes = 4 //gives the expected number of nodes to start Init on
	go func() {              //checks if the num of nodes register is equal to the expected no
		// if it is we start Init
		broker.mu.Lock()
		for len(broker.nodes) < broker.expectedNodes {
			broker.cond.Wait()
		}
		initNodes(broker.nodes)
		broker.Leader = broker.nodes[0]
		broker.mu.Unlock()
	}()
	rpc.Register(broker)
	listener, err := net.Listen("tcp", ":"+*pAddr)
	if err != nil {
		fmt.Println("Error starting TCP listener:", err)
		return
	}
	broker.listener = listener
	defer listener.Close()

	fmt.Println("Listening on port:", *pAddr)
	rpc.Accept(listener)
}
