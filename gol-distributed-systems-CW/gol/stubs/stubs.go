package stubs

import "uk.ac.bris.cs/gameoflife/util"

// client to broker
var RunNodesHandler = "BrokerOperations.RunNodes"
var GetAliveCellsHandler = "BrokerOperations.GetAliveCells"
var GetWorldHandler = "BrokerOperations.GetWorld"
var TogglePauseHandler = "BrokerOperations.TogglePause"
var OnClientQuitHandler = "BrokerOperations.OnClientQuit"
var NodeOnClientQuitHandler = "NodeOperations.OnClientQuit"

var ShutdownHandler = "BrokerOperations.Shutdown"

//empty empty

var RegisterNodeHandler = "BrokerOperations.RegisterNode"

type RegisterRequest struct {
	NodeAddress string
}
type StatusReport struct {
	Message string
}

// node to broker
var OnNodeCompletionHandler = "BrokerOperations.OnNodeCompletion"

type NodeCompletionRequest struct {
	World      [][]byte
	AliveCells []util.Cell
	NodeID     int
}

//empty response

// broker to node
var InitNodeHandler = "NodeOperations.Init" //finished
var SafeModeOnHandler = "NodeOperations.SafeModeOn"
var SafeModeOffHandler = "NodeOperations.SafeModeOff"
var NodeGetAliveCellsHandler = "NodeOperations.GetAliveCells"
var NodeGetWorldHandler = "NodeOperations.GetWorld"
var TogglePauseNodeHandler = "NodeOperations.TogglePause"
var RunNodeHandler = "NodeOperations.RunNode"
var NodeShutdownHandler = "NodeOperations.Shutdown"
var InitRunHandler = "NodeOperations.InitRun"
var NodeTogglePauseHandler = "NodeOperations.TogglePause"

// node to node
var GetBorderHandler = "NodeOperations.GetBorder"

type TurnResponse struct {
	Turn int
}

type BorderRequest struct {
	Border     []byte
	SafeDelete int
	Turn       int
	StopRun    bool
}

type BorderResponse struct {
	Border []byte
	Status string
}

type InfoRequest struct {
	Turn int
}
type NodeWorldResponse struct {
	World [][]byte
}

type InitNodeRequest struct {
	World         [][]byte
	Width         int
	Height        int
	Turn          int
	AboveNode     string
	BelowNode     string
	AboveBorder   []byte
	BelowBorder   []byte
	Leader        bool
	NodeNum       int
	NodeID        int
	BrokerAddress string
}
type InitNodeResponse struct {
}

type InitRunRequest struct {
	World       [][]byte
	Width       int
	Height      int
	AboveBorder []byte
	BelowBorder []byte
	Turns       int
}
type RunNodeRequest struct {
	World       [][]byte
	Turns       int
	Width       int
	Height      int
	AboveBorder []byte
	BelowBorder []byte
}
type RunNodeResponse struct {
	World      [][]byte
	AliveCells []util.Cell
	Turn       int
}

type RunResponse struct {
	World      [][]byte
	AliveCells []util.Cell
	Turn       int
}

type WorldResponse struct {
	World [][]byte
	Turn  int
}

type AliveCellsResponse struct {
	AliveCells int
	Turn       int
}
type EmptyRequest struct{}
type EmptyResponse struct{}

type PauseResponse struct {
	IsPaused bool
	Turn     int
}
type QuitResponse struct {
	Turn int
}
type RunRequest struct {
	World       [][]byte
	ImageWidth  int
	ImageHeight int
	Turns       int
}
