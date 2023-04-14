#!/bin/bash

cwd=$(pwd)
go build -o maelstrom-echo
cd $MAELSTROM_PATH
./maelstrom test -w echo --bin ~/go/bin/maelstrom-echo --node-count 1 --time-limit 10
cd $cwd
