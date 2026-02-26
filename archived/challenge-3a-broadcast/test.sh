#!/bin/bash

cwd=$(pwd)
go build -o maelstrom-broadcast
cd $MAELSTROM_PATH
./maelstrom test -w broadcast --bin ~/go/bin/maelstrom-broadcast --node-count 1 --time-limit 20 --rate 10
cd $cwd
