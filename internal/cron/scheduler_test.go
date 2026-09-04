package cron_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"WorkBaby/internal/cron"
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

func newTestScheduler(t *testing.T) (*cron.Scheduler, *repo.CronJobRepo) {
	t.Helper()
	r := repo.NewCronJobRepo(newTestDB(t))
	return cron.NewScheduler(r), r
}

// TestTriggerExecutesHandlerAndRecordsStatus 调度触发 → handler 执行 → 状态与执行时间落库。
func TestTriggerExecutesHandlerAndRecordsStatus(t *testing.T) {
	s, r := newTestScheduler(t)
	ctx := context.Background()
	var called atomic.Bool
	var gotArgs string
	s.RegisterHandler(domain.ActionRunWorkflow, func(_ context.Context, args json.RawMessage) error {
		called.Store(true)
		gotArgs = string(args)
		return nil
	})

	created, err := s.Create(ctx, domain.CronJobREQ{Name: "job", Schedule: "0 9 * * *", WorkflowID: "WF_1"})
	require.NoError(t, err)
	require.NoError(t, s.Trigger(ctx, created.ID))

	require.Eventually(t, func() bool { return called.Load() }, 2*time.Second, 10*time.Millisecond)
	assert.Contains(t, gotArgs, "WF_1")

	row, err := r.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "success", row.LastStatus)
	require.NotNil(t, row.LastRunAt)
}
