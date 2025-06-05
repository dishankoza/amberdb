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
	"google.golang.org/grpc/credentials/insecure"
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

	if tx.State != Preparing {
		return fmt.Errorf("transaction %s in invalid state for prepare: %v", txID, tx.State)
	}

	// Group writes by shard
	writesByNode := make(map[string][]*amberpb.WriteRequest)
	shardByNode := make(map[string]*metastore.Shard)
	for _, write := range writes {
		if write == nil {
			return fmt.Errorf("invalid write request: nil")
		}
		if write.Key == "" {
			return fmt.Errorf("invalid write request: empty key")
		}

		shard, err := metastore.GetShardForKey(write.Key)
		if err != nil {
			return fmt.Errorf("failed to get shard for key %s: %w", write.Key, err)
		}
		if shard.Primary == "" {
			return fmt.Errorf("shard %s has no primary node", shard.ID)
		}

		writesByNode[shard.Primary] = append(writesByNode[shard.Primary], write)
		shardByNode[shard.Primary] = shard

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
		s := shardByNode[nodeAddr]
		wg.Add(1)
		go func(addr string, writes []*amberpb.WriteRequest, shard *metastore.Shard) {
			defer wg.Done()

			candidates := append([]string{addr}, shard.Nodes...)
			seen := map[string]struct{}{}
			var lastErr error

			for _, cand := range candidates {
				if _, dup := seen[cand]; dup {
					continue
				}
				seen[cand] = struct{}{}

				dialCtx, cancel := context.WithTimeout(ctx, c.timeout)
				conn, err := grpc.DialContext(dialCtx, cand,
					grpc.WithTransportCredentials(insecure.NewCredentials()))
				cancel()
				if err != nil {
					lastErr = err
					continue
				}

				client := amberpb.NewAmberServiceClient(conn)

				// Begin txn on this replica
				localTx, err := client.BeginTransaction(ctx, &amberpb.Empty{})
				if err != nil {
					conn.Close()
					lastErr = err
					continue
				}

				ok := true
				for _, w := range writes {
					w.TxId = localTx.Id
					st, err := client.Write(ctx, w)
					if err != nil || !st.Success {
						// follower answered → try next replica
						if st.GetMessage() == "not the leader" {
							ok = false
							break
						}
						conn.Close()
						errChan <- fmt.Errorf("write failed on %s: %v %s",
							cand, err, st.GetMessage())
						return
					}
				}
				if ok {
					shard.Primary = cand
					p := tx.Participants[shard.ID]
					p.Address = cand
					p.State = Prepared
					p.TxID = localTx.Id
					conn.Close()
					return
				}
				conn.Close()
			}
			// All replicas rejected us
			errChan <- fmt.Errorf("no leader found for shard %s: %v",
				shard.ID, lastErr)
		}(nodeAddr, nodeWrites, s)
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
		state := tx.State
		tx.mu.Unlock()
		return fmt.Errorf("transaction %s not in prepared state (current state: %v)", txID, state)
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

			// Create context with timeout
			dialCtx, cancel := context.WithTimeout(ctx, c.timeout)
			defer cancel()

			conn, err := grpc.DialContext(dialCtx, p.Address,
				grpc.WithTransportCredentials(insecure.NewCredentials()))
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
	if tx.State == Committed {
		tx.mu.Unlock()
		return fmt.Errorf("cannot abort committed transaction: %s", txID)
	}
	if tx.State == Aborted {
		tx.mu.Unlock()
		return nil
	}
	tx.State = Aborting
	tx.mu.Unlock()

	// Abort phase: send abort to all participants
	var wg sync.WaitGroup
	errChan := make(chan error, len(tx.Participants))

	for _, participant := range tx.Participants {
		if participant.TxID == "" {
			continue
		}

		wg.Add(1)
		go func(p *ParticipantInfo) {
			defer wg.Done()

			// Create context with timeout
			dialCtx, cancel := context.WithTimeout(ctx, c.timeout)
			defer cancel()

			conn, err := grpc.DialContext(dialCtx, p.Address,
				grpc.WithTransportCredentials(insecure.NewCredentials()))
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
