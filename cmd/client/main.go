package main

import (
	"context"
	"fmt"
	"log"
	"time"

	amberpb "github.com/dishankoza/amberdb/proto"
	"google.golang.org/grpc"
)

func main() {
	conn, err := grpc.Dial("node1:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("failed to dial: %v", err)
	}
	client := amberpb.NewAmberServiceClient(conn)

	// Begin transaction
	txn, err := client.BeginTransaction(context.Background(), &amberpb.Empty{})
	if err != nil {
		log.Fatalf("BeginTransaction error: %v", err)
	}
	fmt.Printf("Started Txn: %s\n", txn.Id)

	// Write key1
	status, err := client.Write(context.Background(), &amberpb.WriteRequest{Key: "key1", Value: "value1", TxId: txn.Id})
	if err != nil || !status.Success {
		log.Fatalf("Write error: %v %s", err, status.Message)
	}
	fmt.Println("Write OK")

	// Commit
	status, err = client.Commit(context.Background(), &amberpb.TxnID{Id: txn.Id})
	if err != nil || !status.Success {
		log.Fatalf("Commit error: %v %s", err, status.Message)
	}
	fmt.Println("Commit OK")

	// Read back
	time.Sleep(1 * time.Second)
	readResp, err := client.Read(context.Background(), &amberpb.ReadRequest{Key: "key1"})
	if err != nil {
		log.Fatalf("Read error: %v", err)
	}
	fmt.Printf("Read value: %s\n", readResp.Value)
}
