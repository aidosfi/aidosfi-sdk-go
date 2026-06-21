package aidosfi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClientDefaults(t *testing.T) {
	c := NewClient(AidosConfig{APIKey: "test-key"})
	if c.config.BaseURL != defaultBaseURL {
		t.Errorf("expected baseURL %q, got %q", defaultBaseURL, c.config.BaseURL)
	}
	if c.config.WsURL != defaultWsURL {
		t.Errorf("expected wsURL %q, got %q", defaultWsURL, c.config.WsURL)
	}
	if c.config.Timeout != defaultTimeout {
		t.Errorf("expected timeout %d, got %d", defaultTimeout, c.config.Timeout)
	}
}

func TestNewClientCustomConfig(t *testing.T) {
	c := NewClient(AidosConfig{
		APIKey:  "custom-key",
		BaseURL: "https://custom.api.com",
		WsURL:   "wss://custom.ws.com",
		Timeout: 5000,
	})
	if c.config.BaseURL != "https://custom.api.com" {
		t.Errorf("expected custom baseURL, got %q", c.config.BaseURL)
	}
	if c.config.Timeout != 5000 {
		t.Errorf("expected timeout 5000, got %d", c.config.Timeout)
	}
}

func TestCreateAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing auth header")
		}
		if r.URL.Path != "/v1/accounts" {
			t.Errorf("expected path /v1/accounts, got %s", r.URL.Path)
		}

		var req CreateAccountRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Label != "payroll" {
			t.Errorf("expected label 'payroll', got %q", req.Label)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Account{
			ID:              "acc_abc123",
			Label:           "payroll",
			Asset:           AssetUSDC,
			ShieldedBalance: "zk:0xabc...",
			CreatedAt:       "2026-06-21T00:00:00Z",
		})
	}))
	defer server.Close()

	c := NewClient(AidosConfig{APIKey: "test-key", BaseURL: server.URL})
	acct, err := c.CreateAccount(context.Background(), CreateAccountRequest{
		Label: "payroll",
		Asset: AssetUSDC,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acct.ID != "acc_abc123" {
		t.Errorf("expected id 'acc_abc123', got %q", acct.ID)
	}
	if acct.Asset != AssetUSDC {
		t.Errorf("expected asset USDC, got %q", acct.Asset)
	}
}

func TestCreateAccountError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AidosError{
			Code:    "unauthorized",
			Message: "invalid API key",
			Status:  401,
		})
	}))
	defer server.Close()

	c := NewClient(AidosConfig{APIKey: "bad-key", BaseURL: server.URL})
	_, err := c.CreateAccount(context.Background(), CreateAccountRequest{
		Label: "test",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := err.(AidosError)
	if !ok {
		t.Fatalf("expected AidosError, got %T: %v", err, err)
	}
	if apiErr.Code != "unauthorized" {
		t.Errorf("expected code 'unauthorized', got %q", apiErr.Code)
	}
	if apiErr.Status != 401 {
		t.Errorf("expected status 401, got %d", apiErr.Status)
	}
}

func TestListAccounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(PaginatedResponse[Account]{
			Data: []Account{
				{ID: "acc_1", Label: "savings", Asset: AssetUSDC},
				{ID: "acc_2", Label: "trading", Asset: AssetSOL},
			},
			Cursor:  "cursor_next",
			HasMore: true,
		})
	}))
	defer server.Close()

	c := NewClient(AidosConfig{APIKey: "test-key", BaseURL: server.URL})
	result, err := c.ListAccounts(context.Background(), PaginationParams{Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Data) != 2 {
		t.Errorf("expected 2 accounts, got %d", len(result.Data))
	}
	if result.Data[0].ID != "acc_1" {
		t.Errorf("expected acc_1, got %q", result.Data[0].ID)
	}
	if !result.HasMore {
		t.Error("expected HasMore to be true")
	}
	if result.Cursor != "cursor_next" {
		t.Errorf("expected cursor 'cursor_next', got %q", result.Cursor)
	}
}

func TestDeployAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/accounts/acc_1/agents" {
			t.Errorf("expected path /v1/accounts/acc_1/agents, got %s", r.URL.Path)
		}

		var req DeployAgentRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Strategy != StrategyDCA {
			t.Errorf("expected strategy dca, got %q", req.Strategy)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Agent{
			ID:              "agt_001",
			Strategy:        StrategyDCA,
			Asset:           AssetSOL,
			Amount:          100,
			Interval:        Interval1w,
			Status:          AgentStatusRunning,
			AttestationHash: "0xattest...",
			DeployedAt:      "2026-06-21T00:00:00Z",
		})
	}))
	defer server.Close()

	c := NewClient(AidosConfig{APIKey: "test-key", BaseURL: server.URL})
	agent, err := c.DeployAgent(context.Background(), "acc_1", DeployAgentRequest{
		Strategy: StrategyDCA,
		Asset:    AssetSOL,
		Amount:   100,
		Interval: Interval1w,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.ID != "agt_001" {
		t.Errorf("expected id agt_001, got %q", agent.ID)
	}
	if agent.Status != AgentStatusRunning {
		t.Errorf("expected running, got %q", agent.Status)
	}
}

func TestSwap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/swaps" {
			t.Errorf("expected path /v1/swaps, got %s", r.URL.Path)
		}

		var req SwapRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.From != AssetSOL || req.To != AssetUSDC {
			t.Errorf("expected SOL->USDC, got %s->%s", req.From, req.To)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SwapReceipt{
			TxID:       "tx_swap_001",
			From:       AssetSOL,
			To:         AssetUSDC,
			FromAmount: 10,
			ToAmount:   1450.50,
			Price:      145.05,
			ZKProof:    "zk:swap_proof...",
			SettledAt:  "2026-06-21T00:00:00Z",
		})
	}))
	defer server.Close()

	c := NewClient(AidosConfig{APIKey: "test-key", BaseURL: server.URL})
	receipt, err := c.Swap(context.Background(), SwapRequest{
		From:   AssetSOL,
		To:     AssetUSDC,
		Amount: 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipt.TxID != "tx_swap_001" {
		t.Errorf("expected tx_swap_001, got %q", receipt.TxID)
	}
	if receipt.ToAmount != 1450.50 {
		t.Errorf("expected 1450.50, got %f", receipt.ToAmount)
	}
}

func TestAidosErrorImplementsError(t *testing.T) {
	err := AidosError{Code: "test", Message: "test msg", Status: 400}
	if err.Error() == "" {
		t.Error("Error() returned empty string")
	}
}

func TestWsEventUnmarshal(t *testing.T) {
	raw := []byte(`{"type":"balance_update","accountId":"acc_1","shieldedBalance":"zk:0xdef..."}`)
	var event WsEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if event.Type != "balance_update" {
		t.Errorf("expected balance_update, got %q", event.Type)
	}
	if event.Payload == nil {
		t.Error("expected payload to be non-nil")
	}

	var balEvt BalanceUpdateEvent
	if err := json.Unmarshal(event.Payload, &balEvt); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if balEvt.AccountID != "acc_1" {
		t.Errorf("expected acc_1, got %q", balEvt.AccountID)
	}
}
