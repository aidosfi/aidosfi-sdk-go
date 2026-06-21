package aidosfi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.aidosfi.com"
const defaultWsURL = "wss://ws.aidosfi.com"
const defaultTimeout = 30_000 // milliseconds

// AidosClient is the primary client for the Aidos Fi API.
type AidosClient struct {
	config     AidosConfig
	httpClient *http.Client
}

// NewClient creates a new AidosClient with the given configuration.
// Sensible defaults are applied for omitted fields.
func NewClient(config AidosConfig) *AidosClient {
	if config.BaseURL == "" {
		config.BaseURL = defaultBaseURL
	}
	if config.WsURL == "" {
		config.WsURL = defaultWsURL
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultTimeout
	}
	return &AidosClient{
		config: config,
		httpClient: &http.Client{
			Timeout: time.Duration(config.Timeout) * time.Millisecond,
		},
	}
}

// request is the internal helper for all HTTP calls.
func (c *AidosClient) request(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	url := c.config.BaseURL + path

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("aidosfi: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("aidosfi: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("aidosfi: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("aidosfi: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr AidosError
		if err := json.Unmarshal(respBytes, &apiErr); err != nil {
			return AidosError{
				Code:    "unknown",
				Message: string(respBytes),
				Status:  resp.StatusCode,
			}
		}
		apiErr.Status = resp.StatusCode
		return apiErr
	}

	if result != nil {
		if err := json.Unmarshal(respBytes, result); err != nil {
			return fmt.Errorf("aidosfi: unmarshal response: %w", err)
		}
	}
	return nil
}

// ── Accounts ────────────────────────────────────────────────────

// CreateAccount creates a new shielded account.
func (c *AidosClient) CreateAccount(ctx context.Context, req CreateAccountRequest) (Account, error) {
	var acct Account
	err := c.request(ctx, http.MethodPost, "/v1/accounts", req, &acct)
	return acct, err
}

// GetAccount retrieves a shielded account by ID.
func (c *AidosClient) GetAccount(ctx context.Context, accountID string) (Account, error) {
	var acct Account
	err := c.request(ctx, http.MethodGet, "/v1/accounts/"+accountID, nil, &acct)
	return acct, err
}

// ListAccounts returns a paginated list of shielded accounts.
func (c *AidosClient) ListAccounts(ctx context.Context, params PaginationParams) (PaginatedResponse[Account], error) {
	// Build query string manually for zero-dependency
	path := "/v1/accounts"
	if params.Limit > 0 || params.Cursor != "" {
		path += "?"
		first := true
		if params.Limit > 0 {
			path += fmt.Sprintf("limit=%d", params.Limit)
			first = false
		}
		if params.Cursor != "" {
			if !first {
				path += "&"
			}
			path += fmt.Sprintf("cursor=%s", params.Cursor)
		}
	}
	var result PaginatedResponse[Account]
	err := c.request(ctx, http.MethodGet, path, nil, &result)
	return result, err
}

// ── Deposits ────────────────────────────────────────────────────

// Deposit deposits funds into a shielded account.
func (c *AidosClient) Deposit(ctx context.Context, accountID string, req DepositRequest) (DepositReceipt, error) {
	var receipt DepositReceipt
	err := c.request(ctx, http.MethodPost, "/v1/accounts/"+accountID+"/deposit", req, &receipt)
	return receipt, err
}

// ── Cards ───────────────────────────────────────────────────────

// IssueCard issues a new virtual or physical debit card for an account.
func (c *AidosClient) IssueCard(ctx context.Context, accountID string, req IssueCardRequest) (Card, error) {
	var card Card
	err := c.request(ctx, http.MethodPost, "/v1/accounts/"+accountID+"/cards", req, &card)
	return card, err
}

// GetCard retrieves a card by ID.
func (c *AidosClient) GetCard(ctx context.Context, cardID string) (Card, error) {
	var card Card
	err := c.request(ctx, http.MethodGet, "/v1/cards/"+cardID, nil, &card)
	return card, err
}

// FreezeCard freezes a card, preventing further spend.
func (c *AidosClient) FreezeCard(ctx context.Context, cardID string) (Card, error) {
	var card Card
	err := c.request(ctx, http.MethodPost, "/v1/cards/"+cardID+"/freeze", nil, &card)
	return card, err
}

// UnfreezeCard unfreezes a previously frozen card.
func (c *AidosClient) UnfreezeCard(ctx context.Context, cardID string) (Card, error) {
	var card Card
	err := c.request(ctx, http.MethodPost, "/v1/cards/"+cardID+"/unfreeze", nil, &card)
	return card, err
}

// Spend makes a payment with a card.
func (c *AidosClient) Spend(ctx context.Context, cardID string, req SpendRequest) (SpendReceipt, error) {
	var receipt SpendReceipt
	err := c.request(ctx, http.MethodPost, "/v1/cards/"+cardID+"/spend", req, &receipt)
	return receipt, err
}

// ── Agents ──────────────────────────────────────────────────────

// DeployAgent deploys a TEE-guarded AI agent for an account.
func (c *AidosClient) DeployAgent(ctx context.Context, accountID string, req DeployAgentRequest) (Agent, error) {
	var agent Agent
	err := c.request(ctx, http.MethodPost, "/v1/accounts/"+accountID+"/agents", req, &agent)
	return agent, err
}

// GetAgent retrieves an agent by ID.
func (c *AidosClient) GetAgent(ctx context.Context, agentID string) (Agent, error) {
	var agent Agent
	err := c.request(ctx, http.MethodGet, "/v1/agents/"+agentID, nil, &agent)
	return agent, err
}

// PauseAgent pauses a running agent.
func (c *AidosClient) PauseAgent(ctx context.Context, agentID string) (Agent, error) {
	var agent Agent
	err := c.request(ctx, http.MethodPost, "/v1/agents/"+agentID+"/pause", nil, &agent)
	return agent, err
}

// ResumeAgent resumes a paused agent.
func (c *AidosClient) ResumeAgent(ctx context.Context, agentID string) (Agent, error) {
	var agent Agent
	err := c.request(ctx, http.MethodPost, "/v1/agents/"+agentID+"/resume", nil, &agent)
	return agent, err
}

// StopAgent stops a running or paused agent.
func (c *AidosClient) StopAgent(ctx context.Context, agentID string) (Agent, error) {
	var agent Agent
	err := c.request(ctx, http.MethodPost, "/v1/agents/"+agentID+"/stop", nil, &agent)
	return agent, err
}

// ── Swaps ───────────────────────────────────────────────────────

// Swap executes a darkpool swap between two assets.
func (c *AidosClient) Swap(ctx context.Context, req SwapRequest) (SwapReceipt, error) {
	var receipt SwapReceipt
	err := c.request(ctx, http.MethodPost, "/v1/swaps", req, &receipt)
	return receipt, err
}
