package service

// runEventMapper harness 领域事件 → 前端协议（chat:*）/ 消息块 / tool 消息的唯一映射点。
// Runner 只产领域事件，落块、落 tool 消息、发事件都在这里收口。
// 子 Agent 委派的生命周期事件走独立 chat:subagent-* 通道（复用父 run 的 chat:done 会让前端
// 提前关闭 SSE）；子工具事件推 chat:tool*（带 agent 标签），但不写父 run 的消息块与 tool 历史。

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"WorkBaby/internal/capability"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/pkg"
)

type runEventMapper struct {
	svc            *ChatService
	ctx            context.Context
	ses            *domain.ChatSessionDO
	runID          string
	assistantMsgID string
	runState       *capability.RunState // 能力装配态：技能命中详情在 RunStart 时落事件/块
	runStart       time.Time

	blockSeq  int64          // message_blocks 单调序号（同一事件对应同一 seq）
	toolCalls []llm.ToolCall // 父 run 收集的工具调用（run 结束写 assistant.tool_calls）
	doneEvent map[string]any // 终态事件：延迟到 assistant 落库后由 executeAgent 发出

	// textSeg 当前累积的正文片段（自上次落块以来收到的 content 增量）。
	// 模型一轮里的输出是「叙述 → 调工具 → 叙述 → 调工具」交替的，
	// 只有按工具调用的真实位置把正文落成块，刷新回看时才不会把所有叙述挤到过程之后。
	textSeg strings.Builder
}

// newRunEventMapper 构造一次 run 的事件映射器。
func newRunEventMapper(svc *ChatService, ctx context.Context, ses *domain.ChatSessionDO,
	runID, assistantMsgID string, runState *capability.RunState) *runEventMapper {
	return &runEventMapper{
		svc: svc, ctx: ctx, ses: ses, runID: runID, assistantMsgID: assistantMsgID,
		runState: runState, runStart: time.Now(),
	}
}

// persistBlock 过程块落库（thinking/tool_call/tool_result/artifact/skill/genui），
// 仅父 run 落块；刷新后历史消息据此完整回放执行过程。
func (m *runEventMapper) persistBlock(e harness.Event, kind domain.MessageBlockKind, payload map[string]any) {
	if m.svc.blocks == nil || m.assistantMsgID == "" || e.RunID != m.runID {
		return
	}
	m.blockSeq++
	bs, err := json.Marshal(payload)
	if err != nil {
		return
	}
	row := domain.MessageBlockDO{
		ID:        pkg.NewID(domain.IDMessageBlock),
		MessageID: m.assistantMsgID,
		SessionID: m.ses.ID,
		Seq:       m.blockSeq,
		Kind:      kind,
		Payload:   string(bs),
	}
	if err := m.svc.blocks.Create(m.ctx, &row); err != nil {
		pkg.L.Warn("persist message block failed", "runID", m.runID, "err", err.Error())
	}
}

// flushText 把累积的正文片段落成一个 text 块（空/纯空白不落）。
// 落块时机 = 工具调用之前 与 轮次/run 结束：这样块序列里的正文与工具调用
// 就保持了模型真实的输出顺序，而不是「过程全在前、正文全在后」。
func (m *runEventMapper) flushText(e harness.Event) {
	if m.textSeg.Len() == 0 {
		return
	}
	text := m.textSeg.String()
	m.textSeg.Reset()
	if strings.TrimSpace(text) == "" {
		return
	}
	m.persistBlock(e, domain.BlockText, map[string]any{"text": text})
}

// handle 实现 harness.Sink：单事件映射入口。
func (m *runEventMapper) handle(e harness.Event) {
	s := m.svc
	runID, ses := m.runID, m.ses
	isChild := e.RunID != m.runID
	switch e.Kind {
	case harness.EventRunStart:
		if isChild {
			s.emit(runID, ses.ID, "chat:subagent-start", map[string]any{"sub_run_id": e.RunID, "agent": e.Agent})
			return
		}
		// 技能命中先于首帧正文：时间线叙事顺序为「技能命中 → 工具 → 回答」
		if st := m.runState; st != nil && st.SkillName != "" {
			s.emit(runID, ses.ID, "chat:skill", skillBlockPayload(st))
			m.persistBlock(e, domain.BlockSkill, skillBlockPayload(st))
		}
		s.emit(runID, ses.ID, "chat:stream.start", map[string]any{"model": ses.Model})
	case harness.EventTurnStart:
		// 第 N 轮开始（Turn 从 0 计，这里归一为 1 起）：长任务的轮次推进需要用户可见，
		// 前端据此在流式区标注「第 N 轮」并在时间线按轮分段，而不是一坨无结构的步骤。
		if isChild {
			return
		}
		s.emit(runID, ses.ID, "chat:turn-start", map[string]any{"turn": e.Turn + 1})
	case harness.EventCheckpoint:
		// 检查点已写入（每轮工具回填后）：这是「崩溃/中断后能续跑到哪」的位点。
		// 前端记录最近位点，续跑提示据此说明从哪一轮接着做；失败不影响 run。
		if isChild {
			return
		}
		s.emit(runID, ses.ID, "chat:checkpoint", map[string]any{"turn": e.Turn + 1})
	case harness.EventTurnDelta:
		if isChild {
			return
		}
		if p, ok := e.Payload.(harness.TurnDeltaPayload); ok && p.Kind == "content" {
			s.emit(runID, ses.ID, "chat:stream", map[string]any{"delta": p.Text})
			m.textSeg.WriteString(p.Text)
		}
	case harness.EventTurnThinking:
		if isChild {
			return
		}
		if p, ok := e.Payload.(harness.TurnDeltaPayload); ok && p.Kind == "thinking" {
			s.emit(runID, ses.ID, "chat:thinking", map[string]any{"delta": p.Text})
		}
	case harness.EventTurnEnd:
		if isChild {
			return
		}
		// 轮次结束推累计用量（chat:stats）：前端 streamingStats 实时驱动上下文进度与消息用量行
		if p, ok := e.Payload.(harness.UsagePayload); ok {
			s.emit(runID, ses.ID, "chat:stats", map[string]any{
				"turn":                  e.Turn + 1,
				"input_tokens":          p.InputTokens,
				"output_tokens":         p.OutputTokens,
				"cache_read_tokens":     p.CacheRead,
				"cache_creation_tokens": p.CacheWrite,
				"total_tokens":          p.Total,
				"latency_ms":            time.Since(m.runStart).Milliseconds(),
			})
		}
		// 本轮叙述收尾：这一轮若以正文结束（没有后续工具调用），在这里落块
		m.flushText(e)
	case harness.EventToolCall:
		if p, ok := e.Payload.(harness.ToolCallPayload); ok {
			if !isChild {
				// 先落正文再落工具调用：块的 seq 顺序即用户看到的执行顺序
				m.flushText(e)
				m.toolCalls = append(m.toolCalls, llm.ToolCall{
					ID:   p.ID,
					Type: "function",
					Function: llm.FunctionCall{
						Name:      p.Name,
						Arguments: p.Arguments,
					},
				})
			}
			s.emit(runID, ses.ID, "chat:tool", map[string]any{"id": p.ID, "name": p.Name, "arguments": p.Arguments, "agent": e.Agent})
			if !isChild {
				m.persistBlock(e, domain.BlockToolCall, map[string]any{"id": p.ID, "name": p.Name, "arguments": p.Arguments})
			}
		}
	case harness.EventToolStart:
		if p, ok := e.Payload.(harness.ToolCallPayload); ok {
			s.emit(runID, ses.ID, "chat:tool-start", map[string]any{"id": p.ID, "name": p.Name, "agent": e.Agent})
		}
	case harness.EventCompressed:
		// 压缩证据：边界由 harness 按「压缩前后差异」计算，这里落会话元数据 + 提示用户。
		// 用户只会看到「前面的聊天不见了」，不提示就等于静默吞上下文。
		if boundary, ok := e.Payload.(harness.CompressBoundary); ok && !isChild {
			pkg.L.Info("context compressed", "runID", runID,
				"filterKey", boundary.FilterKey, "removed", boundary.RemovedMsgs, "refs", len(boundary.RecoveryRefs))
			s.persistCompressBoundary(m.ctx, ses, boundary)
			s.emit(runID, ses.ID, "chat:compressed", map[string]any{
				"removed_messages": boundary.RemovedMsgs,
				"filter_key":       boundary.FilterKey,
				"cutoff_at":        boundary.CutoffAt,
				"recovery_refs":    boundary.RecoveryRefs,
				"summary":          boundary.Summary,
			})
		}
	case harness.EventToolResult:
		if p, ok := e.Payload.(harness.ToolResultPayload); ok {
			s.emit(runID, ses.ID, "chat:tool-result", map[string]any{
				"id": p.ToolCallID, "name": p.Name,
				"content": p.Content, "error": p.Err, "duration_ms": p.DurationMs,
				"agent": e.Agent, "ui_hint": p.UIHint, "data": p.Data,
			})
			if isChild {
				return
			}
			// 计划快照独立成事件：前端进度卡直接消费，无需解析工具文本
			if p.Name == todoToolName {
				if st, ok := p.Data["session_todo"].(domain.TodoStateRESP); ok {
					s.emit(runID, ses.ID, "chat:todo", map[string]any{"state": st})
				}
			}
			// 结果 + 产物落块：审批拒绝（refused）同样落块，历史可复现完整过程；
			// data 是工具的结构化结果（如 knowledge_search 的命中列表），来源卡直接消费
			m.persistBlock(e, domain.BlockToolResult, map[string]any{
				"tool_call_id": p.ToolCallID, "name": p.Name,
				"content": p.Content, "error": p.Err,
				"duration_ms": p.DurationMs, "refused": p.Refused,
				"ui_hint": p.UIHint, "data": p.Data,
			})
			if len(p.Data) > 0 {
				m.persistBlock(e, domain.BlockArtifact, map[string]any{"name": p.Name, "data": p.Data})
			}
			// 工具结果落库（role=tool），供后续轮次/下次 run 重建上下文
			// 序号统一走分配器：与 steering 注入消息共享水位，避免撞号
			seq, serr := s.allocSeq(m.ctx, ses.ID, 1)
			if serr != nil {
				pkg.L.Warn("alloc tool message seq failed", "err", serr)
			}
			toolMsg := &domain.MessageDO{
				ID:         pkg.NewID(domain.IDMessage),
				SessionID:  ses.ID,
				Seq:        seq,
				RunID:      m.runID,
				Role:       domain.MessageRoleTool,
				Content:    toolResultContent(p),
				ToolCallID: p.ToolCallID,
				Status:     domain.MessageStatusCompleted,
				CreatedAt:  time.Now().UnixMilli(),
				UpdatedAt:  time.Now().UnixMilli(),
			}
			_ = s.messages.Insert(m.ctx, toolMsg)
		}
	case harness.EventError:
		if p, ok := e.Payload.(harness.ErrorPayload); ok {
			if isChild {
				s.emit(runID, ses.ID, "chat:subagent-error", map[string]any{"sub_run_id": e.RunID, "agent": e.Agent, "message": p.Message})
				return
			}
			s.emit(runID, ses.ID, "chat:error", map[string]any{"code": p.Code, "message": p.Message})
		}
	case harness.EventRetry:
		if p, ok := e.Payload.(harness.RetryPayload); ok {
			s.emit(runID, ses.ID, "chat:retry", map[string]any{"attempt": p.Attempt, "delay_ms": p.DelayMs, "reason": p.Reason})
		}
	case harness.EventRunDone:
		if p, ok := e.Payload.(harness.RunDonePayload); ok {
			if isChild {
				s.emit(runID, ses.ID, "chat:subagent-done", map[string]any{
					"sub_run_id": e.RunID, "agent": e.Agent,
					"reason": string(domain.MapHarnessReason(p.Reason)),
				})
				return
			}
			// 收尾前把最后一段正文落块（正常路径已在 TurnEnd 落过，这里是兜底）
			m.flushText(e)
			m.doneEvent = map[string]any{
				"status":      "completed",
				"reason":      string(domain.MapHarnessReason(p.Reason)),
				"stop_reason": p.StopReason,
				"message_id":  p.MessageID,
				"usage":       p.Usage,
			}
		}
	}
}
