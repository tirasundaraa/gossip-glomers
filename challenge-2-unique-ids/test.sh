#!/bin/bash

cwd=$(pwd)
go build -o maelstrom-unique-ids
cd $MAELSTROM_PATH
./maelstrom test -w unique-ids --bin ~/go/bin/maelstrom-unique-ids --time-limit 30 --rate 1000 --node-count 3 --availability total --nemesis partition
cd $cwd
