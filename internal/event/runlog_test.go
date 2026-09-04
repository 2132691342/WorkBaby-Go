package event

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunEventLogReplay(t *testing.T) {
	log := NewRunEventLog(0, 0)
	for i := 1; i <= 5; i++ {
		log.Append("RUN_1", "chat:stream", map[string]any{"n": i})
	}

	events, covered := log.Replay("RUN_1", 0)
	require.True(t, covered)
	require.Len(t, events, 5)

	events, covered = log.Replay("RUN_1", 3)
	require.True(t, covered)
	require.Len(t, events, 2)
	assert.Equal(t, int64(4), events[0].Seq)
	assert.Equal(t, int64(5), events[1].Seq)

	// 未知 run 视为完整重放（无事件可补）
	events, covered = log.Replay("RUN_MISSING", 9)
	assert.True(t, covered)
	assert.Empty(t, events)
}



