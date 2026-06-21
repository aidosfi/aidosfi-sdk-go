package aidosfi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const defaultBaseURL = "https://api.aidosfi.com"
const defaultWsURL = "wss://ws.aidosfi.com"
const defaultTimeout = 30_000 // milliseconds

// AidosClient is the primary client for the Aidos Fi API.
type AidosClient struct {
	config       AidosConfig
	httpClient   *http.Client
	retryCfg     RetryConfig
	idempotency  IdempotencyConfig
	hooks        HooksConfig
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

	retryCfg := DefaultRetryConfig()
	if config.Retry != nil {
		retryCfg = *config.Retry
	}

	idempCfg := IdempotencyConfig{}
	if config.Idempotency != nil {
		idempCfg = *config.Idempotency
	}

	hooksCfg := HooksConfig{}
	if config.Hooks != nil {
		hooksCfg = *config.Hooks
	}

	return &AidosClient{
		config: config,
		httpClient: &http.Client{
			Timeout: time.Duration(config.Timeout) * time.Millisecond,
		},
		retryCfg:    retryCfg,
		idempotency: idempCfg,
		hooks:       hooksCfg,
	}
}

// ── Static: fromEnv ─────────────────────────────────────────────

// NewClientFromEnv creates a new AidosClient using environment variables.
// AIDOSFI_API_KEY is required. Optional: AIDOSFI_BASE_URL, AIDOSFI_WS_URL, AIDOSFI_TIMEOUT.
func NewClientFromEnv() (*AidosClient, error) {
	apiKey, _ := os.LookupEnv("AIDOSFI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("aidosfi: AIDOSFI_API_KEY environment variable is required")
	}

	timeout := defaultTimeout
	if t, ok := os.LookupEnv("AIDOSFI_TIMEOUT"); ok {
		if _, err := fmt.Sscanf(t, "%d", &timeout); err != nil {
			return nil, fmt.Errorf("aidosfi: invalid AIDOSFI_TIMEOUT: %s", t)
		}
	}

	baseURL, _ := os.LookupEnv("AIDOSFI_BASE_URL")
	wsURL, _ := os.LookupEnv("AIDOSFI_WS_URL")

	return NewClient(AidosConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		WsURL:   wsURL,
		Timeout: timeout,
	}), nil
}

// ── Core request with retry, idempotency, hooks ─────────────────

func (c *AidosClient) request(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	maxRetries := c.retryCfg.MaxRetries
	initialDelay := c.retryCfg.InitialDelay
	maxDelay := c.retryCfg.MaxDelay

	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		startTime := time.Now()
		url := c.config.BaseURL + path

		var bodyReader io.Reader
		var bodyBytes []byte
		if body != nil {
			var err error
			bodyBytes, err = json.Marshal(body)
			if err != nil {
				return fmt.Errorf("aidosfi: marshal request body: %w", err)
			}
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return fmt.Errorf("aidosfi: create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		// Idempotency key for mutating requests
		if c.idempotency.Enabled && (method == http.MethodPost || method == http.MethodPut) && body != nil {
			req.Header.Set("Idempotency-Key", generateIdempotencyKey())
		}

		// Hook: onRequest
		if c.hooks.OnRequest != nil {
			headers := make(map[string]string)
			for k, v := range req.Header {
				if len(v) > 0 {
					headers[k] = v[0]
				}
			}
			c.hooks.OnRequest(HookRequest{Method: method, URL: url, Headers: headers})
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			duration := time.Since(startTime)
			// Hook: onError (only on last attempt)
			if attempt >= maxRetries && c.hooks.OnError != nil {
				c.hooks.OnError(HookError{Error: err, URL: url, Duration: duration})
			}
			lastErr = err
			if attempt < maxRetries {
				delayMs := clamp(
					float64(jitter(initialDelay*time.Duration(1<<attempt))),
					0,
					float64(maxDelay),
				)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(delayMs)):
				}
				continue
			}
			return fmt.Errorf("aidosfi: request failed after %d attempts: %w", maxRetries+1, lastErr)
		}

		duration := time.Since(startTime)

		// Hook: onResponse
		if c.hooks.OnResponse != nil {
			c.hooks.OnResponse(HookResponse{Status: resp.StatusCode, URL: url, Duration: duration})
		}

		respBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			if attempt < maxRetries {
				lastErr = fmt.Errorf("aidosfi: read response: %w", err)
				continue
			}
			return fmt.Errorf("aidosfi: read response: %w", err)
		}

		if resp.StatusCode >= 400 {
			var apiErr AidosError
			if err := json.Unmarshal(respBytes, &apiErr); err != nil {
				apiErr = AidosError{
					Code:    "unknown",
					Message: string(respBytes),
					Status:  resp.StatusCode,
				}
			}
			apiErr.Status = resp.StatusCode

			// Respect Retry-After header for 429
			if resp.StatusCode == 429 && attempt < maxRetries {
				if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
					if waitDur, ok := parseRetryAfter(retryAfter); ok {
						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-time.After(waitDur):
						}
						lastErr = apiErr
						continue
					}
				}
			}

			// Retry on 5xx or 429
			if isRetryableStatus(resp.StatusCode) && attempt < maxRetries {
				delayMs := clamp(
					float64(jitter(initialDelay*time.Duration(1<<attempt))),
					0,
					float64(maxDelay),
				)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(delayMs)):
				}
				lastErr = apiErr
				continue
			}

			return apiErr
		}

		if resp.StatusCode == 204 || result == nil {
			return nil
		}

		if err := json.Unmarshal(respBytes, result); err != nil {
			return fmt.Errorf("aidosfi: unmarshal response: %w", err)
		}
		return nil
	}

	if lastErr != nil {
		if apiErr, ok := lastErr.(AidosError); ok {
			return apiErr
		}
		return fmt.Errorf("aidosfi: request failed after retries: %w", lastErr)
	}
	return fmt.Errorf("aidosfi: request failed after retries")
}

// ── Idempotency key generator ──────────────────────────────────

func generateIdempotencyKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "aidos-" + hex.EncodeToString(b)
}

// ── Health ──────────────────────────────────────────────────────

// Health checks the API /v1/health endpoint.
func (c *AidosClient) Health(ctx context.Context) (HealthResponse, error) {
	var hr HealthResponse
	err := c.request(ctx, http.MethodGet, "/v1/health", nil, &hr)
	return hr, err
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
	path := "/v1/accounts"
	path = appendPaginationQuery(path, params)
	var result PaginatedResponse[Account]
	err := c.request(ctx, http.MethodGet, path, nil, &result)
	return result, err
}

// ListAllAccounts collects all accounts across all pages.
func (c *AidosClient) ListAllAccounts(ctx context.Context, limit int) ([]Account, error) {
	var all []Account
	cursor := ""
	for {
		params := PaginationParams{Limit: limit, Cursor: cursor}
		page, err := c.ListAccounts(ctx, params)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		if !page.HasMore || page.Cursor == "" {
			break
		}
		cursor = page.Cursor
	}
	return all, nil
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

// ListCards returns a paginated list of cards for an account.
func (c *AidosClient) ListCards(ctx context.Context, accountID string, params PaginationParams) (PaginatedResponse[Card], error) {
	path := "/v1/accounts/" + accountID + "/cards"
	path = appendPaginationQuery(path, params)
	var result PaginatedResponse[Card]
	err := c.request(ctx, http.MethodGet, path, nil, &result)
	return result, err
}

// ListAllCards collects all cards across all pages for an account.
func (c *AidosClient) ListAllCards(ctx context.Context, accountID string, limit int) ([]Card, error) {
	var all []Card
	cursor := ""
	for {
		params := PaginationParams{Limit: limit, Cursor: cursor}
		page, err := c.ListCards(ctx, accountID, params)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		if !page.HasMore || page.Cursor == "" {
			break
		}
		cursor = page.Cursor
	}
	return all, nil
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

// ListAgents returns a paginated list of agents for an account.
func (c *AidosClient) ListAgents(ctx context.Context, accountID string, params PaginationParams) (PaginatedResponse[Agent], error) {
	path := "/v1/accounts/" + accountID + "/agents"
	path = appendPaginationQuery(path, params)
	var result PaginatedResponse[Agent]
	err := c.request(ctx, http.MethodGet, path, nil, &result)
	return result, err
}

// ListAllAgents collects all agents across all pages for an account.
func (c *AidosClient) ListAllAgents(ctx context.Context, accountID string, limit int) ([]Agent, error) {
	var all []Agent
	cursor := ""
	for {
		params := PaginationParams{Limit: limit, Cursor: cursor}
		page, err := c.ListAgents(ctx, accountID, params)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		if !page.HasMore || page.Cursor == "" {
			break
		}
		cursor = page.Cursor
	}
	return all, nil
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

// ── Helpers ─────────────────────────────────────────────────────

// appendPaginationQuery appends ?limit=N&cursor=X to a path if params are set.
func appendPaginationQuery(path string, params PaginationParams) string {
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
			path += "cursor=" + params.Cursor
		}
	}
	return path
}
