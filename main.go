package main

import (
	"flag"
	raft2 "github.com/Nonsensersunny/simpleraft/raft"
	"math/rand"
	"os"
	"strings"
	"time"
)

func main() {
	port := flag.Int("port", 9090, "rpc port")
	cluster := flag.String("cluster", "localhost:9090", "comma separator")
	id := flag.Int("id", 1, "node id")

	flag.Parse()
	clusters := strings.Split(*cluster, ",")
	nodes := make(map[int]*raft2.Node)
	for i, v := range clusters {
		nodes[i] = raft2.NewNode(v)
	}

	raft := &raft2.Raft{}
	raft.Id = *id
	raft.Nodes = nodes

	raft.RPC(*port)
	raft.Start()
	raft.PrintLog()

	//randomShutdown()

	select {}
}

func randomShutdown() {
	rand.Seed(time.Now().UnixNano())
	go func() {
		for {
			r := rand.Intn(1000)
			if r > 990 {
				os.Exit(1)
			}
			time.Sleep(time.Millisecond * 200)
		}
	}()
}
