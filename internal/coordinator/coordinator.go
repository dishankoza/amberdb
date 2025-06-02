package coordinator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dishankoza/amberdb/internal/metastore"
	amberpb "github.com/dishankoza/amberdb/proto"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// TransactionState tracks the state of a distributed transaction
type TransactionState int

const (
	Preparing TransactionState = iota
	Prepared
	Committing
	Committed
	Aborting
	Aborted
)

// ParticipantInfo tracks the state of each participant in a transaction
type ParticipantInfo struct {
	Address string
	State   TransactionState
	TxID    string // Local transaction ID on this participant
}

// Transaction represents a distributed transaction
type Transaction struct {
	ID           string
	Participants map[string]*ParticipantInfo // shard ID -> participant info
	State        TransactionState
	Timestamp    string
	mu           sync.RWMutex
}

// Coordinator manages distributed transactions
type Coordinator struct {
	transactions map[string]*Transaction
	mu           sync.RWMutex
	timeout      time.Duration
}

// NewCoordinator creates a new transaction coordinator
func NewCoordinator(timeout time.Duration) *Coordinator {
	return &Coordinator{
		transactions: make(map[string]*Transaction),
		timeout:      timeout,
	}
}

// BeginTransaction starts a new distributed transaction
func (c *Coordinator) BeginTransaction() string {
	txID := uuid.New().String()
	c.mu.Lock()
	c.transactions[txID] = &Transaction{
		ID:           txID,
		Participants: make(map[string]*ParticipantInfo),
		State:        Preparing,
		Timestamp:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	c.mu.Unlock()
	return txID
}

// PrepareTransaction executes the prepare phase of 2PC
func (c *Coordinator) PrepareTransaction(ctx context.Context, txID string, writes map[string]*amberpb.WriteRequest) error {
	c.mu.RLock()
	tx, exists := c.transactions[txID]
	c.mu.RUnlock()
	if !exists {
		return fmt.Errorf("transaction not found: %s", txID)
	}

	tx.mu.Lock()
	defer tx.mu.Unlock()

	// Group writes by shard
	writesByNode := make(map[string][]*amberpb.WriteRequest)
	for _, write := range writes {
		shard, err := metastore.GetShardForKey(write.Key)
		if err != nil {
			return fmt.Errorf("failed to get shard for key %s: %w", write.Key, err)
		}

		// Use primary node for writes
		writesByNode[shard.Primary] = append(writesByNode[shard.Primary], write)

		// Track participant
		if _, exists := tx.Participants[shard.ID]; !exists {
			tx.Participants[shard.ID] = &ParticipantInfo{
				Address: shard.Primary,
				State:   Preparing,
			}
		}
	}

	// Prepare phase: send writes to all participants
	var wg sync.WaitGroup
	errChan := make(chan error, len(writesByNode))

	for nodeAddr, nodeWrites := range writesByNode {
		wg.Add(1)
		go func(addr string, writes []*amberpb.WriteRequest) {
			defer wg.Done()

			// Connect to participant
			conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithTimeout(c.timeout))
			if err != nil {
				errChan <- fmt.Errorf("failed to connect to %s: %w", addr, err)
				return
			}
			defer conn.Close()

			client := amberpb.NewAmberServiceClient(conn)

			// Begin local transaction
			localTx, err := client.BeginTransaction(ctx, &amberpb.Empty{})
			if err != nil {
				errChan <- fmt.Errorf("failed to begin transaction on %s: %w", addr, err)
				return
			}

			// Execute writes
			for _, write := range writes {
				write.TxId = localTx.Id
				status, err := client.Write(ctx, write)
				if err != nil || !status.Success {
					errChan <- fmt.Errorf("write failed on %s: %v %s", addr, err, status.GetMessage())
					return
				}
			}

			// Update participant state
			for _, p := range tx.Participants {
				if p.Address == addr {
					p.State = Prepared
					p.TxID = localTx.Id
				}
			}
		}(nodeAddr, nodeWrites)
	}

	// Wait for all prepare operations to complete
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Check for any errors
	for err := range errChan {
		if err != nil {
			tx.State = Aborting
			return fmt.Errorf("prepare phase failed: %w", err)
		}
	}

	tx.State = Prepared
	return nil
}

// CommitTransaction executes the commit phase of 2PC
func (c *Coordinator) CommitTransaction(ctx context.Context, txID string) error {
	c.mu.RLock()
	tx, exists := c.transactions[txID]
	c.mu.RUnlock()
	if !exists {
		return fmt.Errorf("transaction not found: %s", txID)
	}

	tx.mu.Lock()
	if tx.State != Prepared {
		tx.mu.Unlock()
		return fmt.Errorf("transaction %s not in prepared state", txID)
	}
	tx.State = Committing
	tx.mu.Unlock()

	// Commit phase: send commit to all participants
	var wg sync.WaitGroup
	errChan := make(chan error, len(tx.Participants))

	for _, participant := range tx.Participants {
		wg.Add(1)
		go func(p *ParticipantInfo) {
			defer wg.Done()

			conn, err := grpc.Dial(p.Address, grpc.WithInsecure(), grpc.WithTimeout(c.timeout))
			if err != nil {
				errChan <- fmt.Errorf("failed to connect to %s: %w", p.Address, err)
				return
			}
			defer conn.Close()

			client := amberpb.NewAmberServiceClient(conn)
			status, err := client.Commit(ctx, &amberpb.TxnID{Id: p.TxID})
			if err != nil || !status.Success {
				errChan <- fmt.Errorf("commit failed on %s: %v %s", p.Address, err, status.GetMessage())
				return
			}

			p.State = Committed
		}(participant)
	}

	// Wait for all commit operations to complete
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Check for any errors
	for err := range errChan {
		if err != nil {
			tx.State = Aborting
			return fmt.Errorf("commit phase failed: %w", err)
		}
	}

	tx.State = Committed
	return nil
}

// AbortTransaction aborts a distributed transaction
func (c *Coordinator) AbortTransaction(ctx context.Context, txID string) error {
	c.mu.RLock()
	tx, exists := c.transactions[txID]
	c.mu.RUnlock()
	if !exists {
		return fmt.Errorf("transaction not found: %s", txID)
	}

	tx.mu.Lock()
	tx.State = Aborting
	tx.mu.Unlock()

	// Abort phase: send abort to all participants
	var wg sync.WaitGroup
	errChan := make(chan error, len(tx.Participants))

	for _, participant := range tx.Participants {
		if participant.TxID == "" {
			continue // Skip participants that haven't started
		}

		wg.Add(1)
		go func(p *ParticipantInfo) {
			defer wg.Done()

			conn, err := grpc.Dial(p.Address, grpc.WithInsecure(), grpc.WithTimeout(c.timeout))
			if err != nil {
				errChan <- fmt.Errorf("failed to connect to %s: %w", p.Address, err)
				return
			}
			defer conn.Close()

			client := amberpb.NewAmberServiceClient(conn)
			status, err := client.Abort(ctx, &amberpb.TxnID{Id: p.TxID})
			if err != nil || !status.Success {
				errChan <- fmt.Errorf("abort failed on %s: %v %s", p.Address, err, status.GetMessage())
				return
			}

			p.State = Aborted
		}(participant)
	}

	// Wait for all abort operations to complete
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Log any errors but continue with abort
	for err := range errChan {
		if err != nil {
			fmt.Printf("Warning during abort: %v\n", err)
		}
	}

	tx.State = Aborted
	return nil
}

// GetTransactionState returns the current state of a transaction
func (c *Coordinator) GetTransactionState(txID string) (TransactionState, error) {
	c.mu.RLock()
	tx, exists := c.transactions[txID]
	c.mu.RUnlock()

	if !exists {
		return 0, fmt.Errorf("transaction not found: %s", txID)
	}

	tx.mu.RLock()
	defer tx.mu.RUnlock()
	return tx.State, nil
}

// CleanupTransaction removes a completed transaction from memory
func (c *Coordinator) CleanupTransaction(txID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.transactions, txID)
}
