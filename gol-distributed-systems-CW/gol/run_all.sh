#!/bin/bash

#!/bin/bash

# Start the broker in a new terminal window
osascript -e 'tell application "Terminal" to do script "cd ~/Users/aravraja/gol-distributed-systems-CW/gol/broker && go run broker.go"'

# Wait for broker initialization
sleep 2

# Start the server nodes in new terminal windows
num_nodes=30
base_port=8050

for ((i=0; i<num_nodes; i++))
do
  port=$((base_port + i))
  osascript -e 'tell application "Terminal" to do script "cd ~/Users/aravraja/gol-distributed-systems-CW/gol/server && go run server.go '$port'"'
  sleep 0.5
done