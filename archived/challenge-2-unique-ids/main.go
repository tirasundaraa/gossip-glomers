package main

import (
	"log"
	"time"

	"github.com/bwmarrin/snowflake"
	"github.com/google/uuid"
	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func main() {
	n := maelstrom.NewNode()

	// snowflakeNode, err := snowflake.NewNode(n.ID())
	// if err != nil {
	// 	log.Fatal(err)
	// }

	n.Handle("generate", func(msg maelstrom.Message) error {
		var body map[string]any = make(map[string]any)

		body["type"] = "generate_ok"
		// body["id"] = generateUuidStr() // google uuid. PASS
		body["id"] = generateObjectIdStr() // mongo objectId. PASS
		// body["id"] = generateSnowflakeId(snowflakeNode) // Twitter's snowflake. Didn't work, we need to pass the Node ID dynamically
		// body["id"] = generateIdByTimestamp() // timestamp unix-microseconds. Didn't work
		// {
		// "type": "generate_ok",
		// "id": 1234124
		// }

		return n.Reply(msg, body)
	})

	if err := n.Run(); err != nil {
		log.Fatal(err)
	}
}

func generateUuidStr() string {
	return uuid.New().String()
}

func generateObjectIdStr() string {
	return primitive.NewObjectID().Hex()
}

func generateIdByTimestamp() int64 {
	return time.Now().UnixMicro()
}

func generateSnowflakeId(node *snowflake.Node) int64 {
	return node.Generate().Int64()
}
