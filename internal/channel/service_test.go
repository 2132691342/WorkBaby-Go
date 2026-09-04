package channel_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"WorkBaby/internal/channel"
	"WorkBaby/internal/channel/webhook"
	"WorkBaby/internal/db"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/repo"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db") + "?_pragma=journal_mode(MEMORY)&_pragma=busy_timeout(5000)"
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Migrate(gdb))
	t.Cleanup(func() {
		sqlDB, err := gdb.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return gdb
}

// TestSendWebhookLogsSuccess 通道发送 → 命中真实 webhook → 落 success 日志。
func TestSendWebhookLogsSuccess(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	gdb := newTestDB(t)
	reg := channel.NewRegistry()
	require.NoError(t, reg.Register(webhook.New()))
	svc := channel.NewService(repo.NewChannelConfigRepo(gdb), repo.NewChannelMessageLogRepo(gdb), reg)
	ctx := context.Background()

	ch, err := svc.Create(ctx, domain.ChannelConfigREQ{
		ChannelType: domain.ChannelTypeWebhook,
		ConfigJSON:  `{"url":"` + srv.URL + `"}`,
	})
	require.NoError(t, err)

	logID, err := svc.Send(ctx, ch.ID, "text", "hello world")
	require.NoError(t, err)
	assert.NotEmpty(t, logID)
	assert.Contains(t, string(gotBody), "hello world")

	msgs, err := svc.Messages(ctx, ch.ID, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "success", msgs[0].Status)
	assert.Equal(t, "hello world", msgs[0].ContentSummary)
}

// TestSendWebhookLogsFailure 远端 500 → 发送失败并落 failed 日志。
func TestSendWebhookLogsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	gdb := newTestDB(t)
	reg := channel.NewRegistry()
	require.NoError(t, reg.Register(webhook.New()))
	svc := channel.NewService(repo.NewChannelConfigRepo(gdb), repo.NewChannelMessageLogRepo(gdb), reg)
	ctx := context.Background()

	ch, err := svc.Create(ctx, domain.ChannelConfigREQ{
		ChannelType: domain.ChannelTypeWebhook,
		ConfigJSON:  `{"url":"` + srv.URL + `"}`,
	})
	require.NoError(t, err)

	_, err = svc.Send(ctx, ch.ID, "text", "boom")
	require.Error(t, err)

	msgs, err := svc.Messages(ctx, ch.ID, 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "failed", msgs[0].Status)
	assert.NotEmpty(t, msgs[0].ErrorMessage)
}
