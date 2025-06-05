// package main

// import (
// 	"bytes"
// 	"encoding/json"
// 	"fmt"
// 	"io"
// 	"log"
// 	"net/http"
// )

// // MetaserviceClient handles interactions with the metaservice
// type MetaserviceClient struct {
// 	baseURL string
// 	client  *http.Client
// }

// // Write represents a key-value write operation
// type Write struct {
// 	Key   string `json:"key"`
// 	Value string `json:"value"`
// }

// // PrepareRequest represents a transaction prepare request
// type PrepareRequest struct {
// 	TransactionID string            `json:"transaction_id"`
// 	Writes        map[string]*Write `json:"writes"`
// }

// // CommitRequest represents a transaction commit request
// type CommitRequest struct {
// 	TransactionID string `json:"transaction_id"`
// }

// // BeginResponse represents the response from begin transaction
// type BeginResponse struct {
// 	TransactionID string `json:"transaction_id"`
// }

// func NewMetaserviceClient(baseURL string) *MetaserviceClient {
// 	return &MetaserviceClient{
// 		baseURL: baseURL,
// 		client:  &http.Client{},
// 	}
// }

// // BeginTransaction starts a new transaction
// func (mc *MetaserviceClient) BeginTransaction() (string, error) {
// 	resp, err := mc.client.Post(mc.baseURL+"/transaction/begin", "application/json", nil)
// 	if err != nil {
// 		return "", fmt.Errorf("failed to begin transaction: %v", err)
// 	}
// 	defer resp.Body.Close()

// 	var result BeginResponse
// 	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
// 		return "", fmt.Errorf("failed to decode response: %v", err)
// 	}

// 	return result.TransactionID, nil
// }

// // PrepareTxn prepares a transaction with the given writes
// func (mc *MetaserviceClient) PrepareTxn(txID string, writes map[string]*Write) error {
// 	prepareReq := PrepareRequest{
// 		TransactionID: txID,
// 		Writes:        writes,
// 	}

// 	jsonData, err := json.Marshal(prepareReq)
// 	if err != nil {
// 		return fmt.Errorf("failed to marshal prepare request: %v", err)
// 	}

// 	resp, err := mc.client.Post(
// 		mc.baseURL+"/transaction/prepare",
// 		"application/json",
// 		bytes.NewBuffer(jsonData),
// 	)
// 	if err != nil {
// 		return fmt.Errorf("failed to prepare transaction: %v", err)
// 	}
// 	defer resp.Body.Close()

// 	body, _ := io.ReadAll(resp.Body)
// 	if resp.StatusCode != http.StatusOK {
// 		return fmt.Errorf("prepare failed with status %d: %s", resp.StatusCode, string(body))
// 	}

// 	fmt.Printf("Prepare successful: %s\n", string(body))
// 	return nil
// }

// // CommitTxn commits a prepared transaction
// func (mc *MetaserviceClient) CommitTxn(txID string) error {
// 	commitReq := CommitRequest{
// 		TransactionID: txID,
// 	}

// 	jsonData, err := json.Marshal(commitReq)
// 	if err != nil {
// 		return fmt.Errorf("failed to marshal commit request: %v", err)
// 	}

// 	resp, err := mc.client.Post(
// 		mc.baseURL+"/transaction/commit",
// 		"application/json",
// 		bytes.NewBuffer(jsonData),
// 	)
// 	if err != nil {
// 		return fmt.Errorf("failed to commit transaction: %v", err)
// 	}
// 	defer resp.Body.Close()

// 	body, _ := io.ReadAll(resp.Body)
// 	if resp.StatusCode != http.StatusOK {
// 		return fmt.Errorf("commit failed with status %d: %s", resp.StatusCode, string(body))
// 	}

// 	fmt.Printf("Commit successful: %s\n", string(body))
// 	return nil
// }

// func main() {
// 	client := NewMetaserviceClient("http://localhost:8080")

// 	// Begin transaction
// 	fmt.Println("=== Beginning Transaction ===")
// 	txID, err := client.BeginTransaction()
// 	if err != nil {
// 		log.Fatalf("Failed to begin transaction: %v", err)
// 	}
// 	fmt.Printf("Transaction ID: %s\n", txID)

// 	// Prepare writes
// 	writes := map[string]*Write{
// 		"a": {Key: "a", Value: "apple"},
// 		"b": {Key: "b", Value: "banana"},
// 	}

// 	// Prepare transaction
// 	fmt.Println("\n=== Preparing Transaction ===")
// 	if err := client.PrepareTxn(txID, writes); err != nil {
// 		log.Fatalf("Failed to prepare transaction: %v", err)
// 	}

// 	// Commit transaction
// 	fmt.Println("\n=== Committing Transaction ===")
// 	if err := client.CommitTxn(txID); err != nil {
// 		log.Fatalf("Failed to commit transaction: %v", err)
// 	}
// }

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
)

func mustParse(s string) *url.URL { u, _ := url.Parse(s); return u }

// ---------- helpers ----------
func mustDo(req *http.Request) *http.Response {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("request failed: %v", err)
	}
	return resp
}

// ---------- main ----------
func main() {
	meta := flag.String("meta", "http://192.168.64.5:8080", "metaservice URL")
	key := flag.String("key", "x", "KV key to write")
	val := flag.String("val", "value1", "KV value to write")
	flag.Parse()

	/* 1 ─── Begin ──────────────────────────────────────────── */
	beginResp := struct {
		TransactionID string `json:"transaction_id"`
	}{}
	resp := mustDo(&http.Request{
		Method: "POST",
		URL:    mustParse(*meta + "/transaction/begin"),
	})
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&beginResp); err != nil {
		log.Fatalf("decode begin: %v", err)
	}
	fmt.Println("Started Txn:", beginResp.TransactionID)

	/* 2 ─── Prepare ────────────────────────────────────────── */
	prepBody, _ := json.Marshal(map[string]any{
		"transaction_id": beginResp.TransactionID,
		"writes": map[string]any{
			*key: map[string]string{
				"key":   *key,
				"value": *val,
			},
		},
	})
	req, _ := http.NewRequest("POST", *meta+"/transaction/prepare",
		bytes.NewReader(prepBody))
	req.Header.Set("Content-Type", "application/json")

	if prep := mustDo(req); prep.StatusCode != 200 {
		log.Fatalf("prepare failed: http %d", prep.StatusCode)
	}
	fmt.Println("Write OK")

	/* 3 ─── Commit ─────────────────────────────────────────── */
	commitBody, _ := json.Marshal(map[string]string{
		"transaction_id": beginResp.TransactionID,
	})
	req, _ = http.NewRequest("POST", *meta+"/transaction/commit",
		bytes.NewReader(commitBody))
	req.Header.Set("Content-Type", "application/json")

	if com := mustDo(req); com.StatusCode != 200 {
		log.Fatalf("commit failed: http %d", com.StatusCode)
	}
	fmt.Println("Commit OK")
}
