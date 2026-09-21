//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/config"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/database"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/repository"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/infrastructure/transport"
	"github.com/ferrariwill/jungle-gaming-challenge/internal/usecase"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

type testStack struct {
	cfg         *config.Config
	pool        *pgxpool.Pool
	tm          *database.TransactionManager
	walletRepo  *repository.WalletRepository
	txRepo      *repository.TransactionRepository
	messageRepo *repository.MessagingRepository
	openUC      *usecase.OpenWalletUsecase
	wagerUC     *usecase.WagerUsecase
	sqsClient   *sqs.Client
}

func requireIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("INTEGRATION") != "1" {
		t.Skip("set INTEGRATION=1 and ensure Postgres, Keycloak and LocalStack are running")
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func applyMigrations(t *testing.T, dbURL string) {
	t.Helper()
	root := moduleRoot(t)
	src := "file://" + filepath.ToSlash(filepath.Join(root, "migrations"))
	m, err := migrate.New(src, dbURL)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	defer m.Close()
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate.Up: %v", err)
	}
}

func newTestStack(t *testing.T) *testStack {
	t.Helper()
	requireIntegration(t)

	_ = os.Setenv("AWS_ACCESS_KEY_ID", "test")
	_ = os.Setenv("AWS_SECRET_ACCESS_KEY", "test")

	cfg := config.NewConfig()
	ctx := context.Background()

	applyMigrations(t, cfg.DBConnectionString)

	pool, err := pgxpool.New(ctx, cfg.DBConnectionString)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("postgres ping: %v", err)
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWSRegion))
	if err != nil {
		t.Fatalf("aws config: %v", err)
	}
	sqsClient := sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		if cfg.SQSEndpoint != "" {
			o.BaseEndpoint = aws.String(cfg.SQSEndpoint)
		}
	})

	tm := database.NewTransactionManager(pool)
	walletRepo := repository.NewWalletRepository(pool)
	txRepo := repository.NewTransactionRepository(pool)
	messageRepo := repository.NewMessagingRepository(pool)

	return &testStack{
		cfg:         cfg,
		pool:        pool,
		tm:          tm,
		walletRepo:  walletRepo,
		txRepo:      txRepo,
		messageRepo: messageRepo,
		openUC:      usecase.NewOpenWalletUseCase(cfg, tm, walletRepo, txRepo, messageRepo),
		wagerUC:     usecase.NewWagerUsecase(cfg, tm, walletRepo, txRepo, messageRepo),
		sqsClient:   sqsClient,
	}
}

// newIndependentInstance simulates another process by wiring a fresh usecase graph on the same DB.
func (s *testStack) newIndependentInstance(t *testing.T) *testStack {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), s.cfg.DBConnectionString)
	if err != nil {
		t.Fatalf("instance pool: %v", err)
	}
	t.Cleanup(pool.Close)

	tm := database.NewTransactionManager(pool)
	walletRepo := repository.NewWalletRepository(pool)
	txRepo := repository.NewTransactionRepository(pool)
	messageRepo := repository.NewMessagingRepository(pool)
	return &testStack{
		cfg:         s.cfg,
		pool:        pool,
		tm:          tm,
		walletRepo:  walletRepo,
		txRepo:      txRepo,
		messageRepo: messageRepo,
		openUC:      usecase.NewOpenWalletUseCase(s.cfg, tm, walletRepo, txRepo, messageRepo),
		wagerUC:     usecase.NewWagerUsecase(s.cfg, tm, walletRepo, txRepo, messageRepo),
		sqsClient:   s.sqsClient,
	}
}

func uniquePlayer(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func (s *testStack) openWallet(t *testing.T, player, amount string) usecase.OpenWalletOutputDTO {
	t.Helper()
	out, err := s.openUC.Execute(context.Background(), usecase.OpenWalletInputDTO{
		PlayerID: player, InitialAmount: amount, InitialCurrency: "BRL",
	})
	if err != nil {
		t.Fatalf("open wallet: %v", err)
	}
	return out
}

func (s *testStack) countLedger(t *testing.T, walletID string) int64 {
	t.Helper()
	var n int64
	err := s.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM wallet_ledger_entries WHERE wallet_id = $1`, walletID).Scan(&n)
	if err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	return n
}

func (s *testStack) countOutboxByStatus(t *testing.T, status string) int64 {
	t.Helper()
	var n int64
	err := s.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM outbox_events WHERE status = $1`, status).Scan(&n)
	if err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	return n
}

func (s *testStack) countInbox(t *testing.T, messageID string) int64 {
	t.Helper()
	var n int64
	err := s.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM inbox_messages WHERE message_id = $1`, messageID).Scan(&n)
	if err != nil {
		t.Fatalf("count inbox: %v", err)
	}
	return n
}

func (s *testStack) countTxByExternal(t *testing.T, providerID, externalID string) int64 {
	t.Helper()
	var n int64
	err := s.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM wager_transactions WHERE provider_id = $1 AND external_transaction_id = $2`,
		providerID, externalID).Scan(&n)
	if err != nil {
		t.Fatalf("count tx: %v", err)
	}
	return n
}

func fetchKeycloakToken(t *testing.T) string {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", "provider-a-service")
	form.Set("client_secret", "super-secret-token-provedor-a")

	req, err := http.NewRequest(http.MethodPost,
		"http://localhost:8080/realms/jungle/protocol/openid-connect/token",
		bytes.NewBufferString(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("keycloak token request: %v (is Keycloak up?)", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("keycloak token status %d: %s", resp.StatusCode, body)
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.AccessToken == "" {
		t.Fatalf("keycloak token parse: %v body=%s", err, body)
	}
	return parsed.AccessToken
}

func (s *testStack) newHTTPHandler(t *testing.T) *transport.HTTPHandler {
	t.Helper()
	auth, err := transport.NewAuthMiddleware(s.cfg)
	if err != nil {
		t.Fatal(err)
	}
	recon := usecase.NewReconciliationUsecase(s.walletRepo)
	return transport.NewHTTPHandler(s.wagerUC, s.openUC, recon, s.walletRepo, auth, s.pool)
}

func (s *testStack) sendWinToLocalStack(t *testing.T, player, walletID, extID string) error {
	t.Helper()
	body := fmt.Sprintf(`{
		"messageId": "msg-%s",
		"type": "WagerTransactionRequested",
		"data": {
			"providerId": "provider-a",
			"externalTransactionId": "%s",
			"idempotencyKey": "provider-a:%s",
			"playerId": "%s",
			"walletId": "%s",
			"kind": "WIN",
			"money": {"amount": "12.00", "currency": "BRL"}
		}
	}`, extID, extID, extID, player, walletID)

	_, err := s.sqsClient.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:               aws.String(s.cfg.WagerQueueURL),
		MessageBody:            aws.String(body),
		MessageGroupId:         aws.String(player),
		MessageDeduplicationId: aws.String(extID + "-dedup"),
	})
	return err
}
