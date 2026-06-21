# AidosFi Go SDK $AIDOS

<div align="center">

**The Autonomous Privacy Bank** — Banking, AI Agents, and Darkpool Execution Without Surveillance.

[![Website](https://img.shields.io/badge/Website-aidosfi.app-6366f1?style=flat-square)](https://aidosfi.app)
[![dApp](https://img.shields.io/badge/dApp-dapp.aidosfi.app-818cf8?style=flat-square)](https://dapp.aidosfi.app)
[![Docs](https://img.shields.io/badge/Docs-docs.aidosfi.app-c4b5fd?style=flat-square)](https://docs.aidosfi.app)
[![Whitepaper](https://img.shields.io/badge/Whitepaper-PDF-e9d5ff?style=flat-square)](https://aidosfi.app/whitepaper.pdf)
[![Telegram](https://img.shields.io/badge/Telegram-t.me%2Faidosfiapp-26a5e4?style=flat-square)](https://t.me/aidosfiapp)
[![X](https://img.shields.io/badge/X-@aidosfi-1d9bf0?style=flat-square)](https://x.com/aidosfi)
[![GitHub](https://img.shields.io/badge/GitHub-aidosfi-181717?style=flat-square)](https://github.com/aidosfi)

</div>

---

```bash
go get github.com/aidosfi/sdk
```

## Features

| Category       | Feature                                      |
|---------------|----------------------------------------------|
| **Accounts**  | Create, get, and list shielded accounts with ZK-committed balances |
| **Deposits**  | Deposit assets (USDC, USDT, EURC, SOL) with ZK proof receipts |
| **Cards**     | Issue virtual/physical cards, freeze/unfreeze, get card details |
| **Spending**  | Make merchant payments via card with real-time settlement |
| **Agents**    | Deploy, pause, resume, stop autonomous TEE-guarded trading agents with 6 strategies |
| **Swaps**     | Execute darkpool asset swaps with configurable slippage (ZK-settled) |
| **WebSocket** | Real-time feed: balance updates, agent status changes, card swipes, swap fills |
| **Pagination**| Cursor-based pagination for list endpoints |
| **Context**   | All methods accept `context.Context` for cancellation and deadlines |
| **Errors**    | `AidosError` struct implementing `error` with code, status, and details |

### Core Types

- **Assets**: `AssetUSDC` | `AssetUSDT` | `AssetEURC` | `AssetSOL`
- **Card Types**: `CardTypeVirtual` | `CardTypePhysical`
- **Agent Strategies**: `StrategyDCA` | `StrategyGrid` | `StrategyYieldMaximizer` | `StrategyRiskParity` | `StrategyMomentum` | `StrategyMeanReversion`
- **Intervals**: `Interval1h` | `Interval6h` | `Interval12h` | `Interval1d` | `Interval1w` | `Interval1m`
- **WebSocket Events**: typed channels for `balance_update`, `agent_update`, `card_swipe`, `swap_fill`

## Quickstart

```go
package main

import (
    "context"
    "fmt"
    "log"

    aidosfi "github.com/aidosfi/sdk"
)

func main() {
    ctx := context.Background()
    client := aidosfi.NewClient(aidosfi.AidosConfig{
        APIKey: "sk_liv...xxxx",
        // BaseURL: "https://api.aidosfi.com",  // default
        // WsURL:   "wss://ws.aidosfi.com",     // default
        // Timeout: 30_000,                      // ms, default
    })

    // ── Accounts ─────────────────────────────────────
    account, err := client.CreateAccount(ctx, aidosfi.CreateAccountRequest{
        Label: "My Vault",
        Asset: aidosfi.AssetUSDC,
    })
    if err != nil {
        log.Fatal(err)
    }
    // Account { ID: "acc_xxx", Label: "My Vault", Asset: AssetUSDC,
    //           ShieldedBalance: "...", CreatedAt: "2025-..." }

    acct, _ := client.GetAccount(ctx, account.ID)
    page, _ := client.ListAccounts(ctx, aidosfi.PaginationParams{Limit: 10})
    fmt.Println(acct.ID, page.HasMore)

    // ── Deposits ─────────────────────────────────────
    receipt, err := client.Deposit(ctx, account.ID, aidosfi.DepositRequest{
        Asset:  aidosfi.AssetUSDC,
        Amount: 5_000,
    })
    // DepositReceipt { TxID: "...", ZkProof: "...", SettledAt: "..." }

    // ── Cards ────────────────────────────────────────
    card, err := client.IssueCard(ctx, account.ID, aidosfi.IssueCardRequest{
        Type:  aidosfi.CardTypeVirtual,
        Limit: 2_000,
        Label: aidosfi.StringPtr("Travel"),
    })
    // Card { ID: "card_xxx", Last4: "1234", Status: CardStatusActive }

    frozen, _ := client.FreezeCard(ctx, card.ID)
    unfrozen, _ := client.UnfreezeCard(ctx, card.ID)
    _ = frozen
    _ = unfrozen

    // ── Spend ────────────────────────────────────────
    spendReceipt, err := client.Spend(ctx, card.ID, aidosfi.SpendRequest{
        Merchant: "Coffee Shop",
        Amount:   4.50,
    })
    // SpendReceipt { TxID: "...", Merchant: "Coffee Shop", SettledAt: "..." }

    // ── Agents ───────────────────────────────────────
    agent, err := client.DeployAgent(ctx, account.ID, aidosfi.DeployAgentRequest{
        Strategy: aidosfi.StrategyDCA,
        Asset:    aidosfi.AssetSOL,
        Amount:   100,
        Interval: aidosfi.Interval1d,
    })
    // Agent { ID: "agent_xxx", Status: AgentStatusRunning, AttestationHash: "..." }

    client.PauseAgent(ctx, agent.ID)
    client.ResumeAgent(ctx, agent.ID)
    client.StopAgent(ctx, agent.ID)

    // ── Swaps ────────────────────────────────────────
    swapReceipt, err := client.Swap(ctx, aidosfi.SwapRequest{
        From:     aidosfi.AssetUSDC,
        To:       aidosfi.AssetSOL,
        Amount:   500,
        Slippage: aidosfi.IntPtr(50),   // basis points (0.5%)
    })
    // SwapReceipt { TxID: "...", FromAmount: 500, ToAmount: 2.5, ZkProof: "..." }

    // ── WebSocket ────────────────────────────────────
    events, errs, done, err := client.ConnectWebSocket(ctx)
    if err != nil {
        log.Fatal(err)
    }
    defer close(done)

    go func() {
        for {
            select {
            case event, ok := <-events:
                if !ok {
                    return
                }
                switch event.Type {
                case "balance_update":
                    fmt.Printf("Balance: %s\n", event.Data.(aidosfi.WSAccountUpdate).ShieldedBalance)
                case "agent_update":
                    fmt.Printf("Agent status: %s\n", event.Data.(aidosfi.WSAgentEvent).Status)
                case "card_swipe":
                    fmt.Printf("Card swipe: %s %.2f\n",
                        event.Data.(aidosfi.WSCardEvent).Merchant,
                        event.Data.(aidosfi.WSCardEvent).Amount)
                case "swap_fill":
                    // ...
                }
            case err := <-errs:
                fmt.Printf("WS error: %v\n", err)
            case <-ctx.Done():
                return
            }
        }
    }()

    fmt.Printf("Spend: %.2f at %s\n", spendReceipt.Amount, spendReceipt.Merchant)
    fmt.Printf("Swap: %.f %s → %.2f %s\n",
        swapReceipt.FromAmount, swapReceipt.From,
        swapReceipt.ToAmount, swapReceipt.To)
}
```

## Error Handling

```go
account, err := client.CreateAccount(ctx, req)
if err != nil {
    var apiErr aidosfi.AidosError
    if errors.As(err, &apiErr) {
        fmt.Println(apiErr.Code)    // "VALIDATION_ERROR"
        fmt.Println(apiErr.Status)  // 400
    }
}
```

## API Reference

### Constructor

```go
func NewClient(config AidosConfig) *AidosClient
```

| Field     | Type     | Required | Default                         |
|----------|----------|----------|----------------------------------|
| `APIKey`  | `string` | Yes      | —                                |
| `BaseURL` | `string` | No       | `https://api.aidosfi.com`       |
| `WsURL`   | `string` | No       | `wss://ws.aidosfi.com`          |
| `Timeout` | `int`    | No       | `30_000` (ms)                   |

### Account Endpoints

```go
func (c *AidosClient) CreateAccount(ctx context.Context, req CreateAccountRequest) (Account, error)
func (c *AidosClient) GetAccount(ctx context.Context, accountID string) (Account, error)
func (c *AidosClient) ListAccounts(ctx context.Context, params PaginationParams) (PaginatedResponse[Account], error)
```

### Deposit

```go
func (c *AidosClient) Deposit(ctx context.Context, accountID string, req DepositRequest) (DepositReceipt, error)
```

### Card Endpoints

```go
func (c *AidosClient) IssueCard(ctx context.Context, accountID string, req IssueCardRequest) (Card, error)
func (c *AidosClient) GetCard(ctx context.Context, cardID string) (Card, error)
func (c *AidosClient) FreezeCard(ctx context.Context, cardID string) (Card, error)
func (c *AidosClient) UnfreezeCard(ctx context.Context, cardID string) (Card, error)
```

### Spend

```go
func (c *AidosClient) Spend(ctx context.Context, cardID string, req SpendRequest) (SpendReceipt, error)
```

### Agent Endpoints

```go
func (c *AidosClient) DeployAgent(ctx context.Context, accountID string, req DeployAgentRequest) (Agent, error)
func (c *AidosClient) GetAgent(ctx context.Context, agentID string) (Agent, error)
func (c *AidosClient) PauseAgent(ctx context.Context, agentID string) (Agent, error)
func (c *AidosClient) ResumeAgent(ctx context.Context, agentID string) (Agent, error)
func (c *AidosClient) StopAgent(ctx context.Context, agentID string) (Agent, error)
```

### Swap

```go
func (c *AidosClient) Swap(ctx context.Context, req SwapRequest) (SwapReceipt, error)
```

### WebSocket

```go
func (c *AidosClient) ConnectWebSocket(ctx context.Context) (events <-chan WSMessage, errs <-chan error, done chan<- struct{}, err error)
```

Returns typed Go channels:  
- `events`: parsed `WSMessage` with `Type` and `Data`  
- `errs`: connection-level errors  
- `done`: send on this channel to cleanly close the connection

## Development

```bash
go build ./...
go test ./... -v     # 9 tests — mock HTTP server + JSON unmarshaling
```

## License

MIT
