package harness

import "sync"

// 内置 Agent 定义表。
//
// chat / workflow / cron 以 name 引用同一个 Definition：一次定义、随处运行，
// 扩展 = 往表里加一条。

// DefaultAgents 返回全部内置 Agent。
func DefaultAgents() []Definition {
	return []Definition{
		{
			Name:        "default",
			Description: "通用全能助手（默认）：记忆开启、可执行工具",
			Persona:     personaDefault,
			Tools: ToolPolicy{
				// 白名单收敛到核心集：functools 纯函数（30+ 个）默认不在此，用户可在设置页开启；
				// 危险命令由 runner 的策略门统一拦截，白名单只控制「带不带」。
				Allow: []string{
					"file_*", "doc_reader", "archive_manager",
					"exec", "run_skill_script", "delegate_task", "run_workflow",
					"websearch", "webfetch", "http",
					"knowledge_search", "memory_write",
					"todo", "request_input",
					"enter_plan_mode", "exit_plan_mode",
				},
			},
			Memory: MemoryPolicy{Enabled: true, RecallLimit: 6, Formation: true},
			Budget: Budget{ContextTokens: 120_000},
		},
		{
			Name:        "coding",
			Description: "工程助手：读代码/改文件/跑命令；无记忆、长循环",
			Persona:     personaCoding,
			Tools:       ToolPolicy{Allow: []string{"exec", "file_*", "doc_reader", "archive", "websearch"}},
			Memory:      MemoryPolicy{Enabled: false, Formation: false},
			Budget:      Budget{MaxTurns: 40},
		},
		{
			Name:        "research",
			Description: "调研助手：联网搜索与本地知识库优先",
			Persona:     personaResearch,
			Tools: ToolPolicy{
				Allow: []string{"websearch", "webfetch", "http", "knowledge_search"},
			},
			Memory: MemoryPolicy{Enabled: true, RecallLimit: 10, Formation: false},
		},
		{
			Name:        "writer",
			Description: "写作助手：轻工具、长输出",
			Persona:     personaWriter,
			Memory:      MemoryPolicy{Enabled: true, RecallLimit: 6, Formation: false},
		},
	}
}

// Agent 按名取 Agent：内置表优先，其后自定义子智能体；未知名回退 default（按名引用不硬失败）。
func Agent(name string) Definition {
	for _, d := range DefaultAgents() {
		if d.Name == name {
			return d
		}
	}
	for _, d := range CustomAgents() {
		if d.Name == name {
			return d
		}
	}
	return DefaultAgents()[0]
}

// customMu 保护自定义子智能体注册表；写方是 AgentProfileService（启动同步 + CRUD 后重同步）。
var customMu sync.RWMutex

// customAgents 当前生效的自定义子智能体（仅 enabled）。
var customAgents []Definition

// SetCustomAgents 整表替换自定义子智能体注册表（builtin 名在同步侧被拒，这里不再校验）。
func SetCustomAgents(defs []Definition) {
	customMu.Lock()
	defer customMu.Unlock()
	customAgents = defs
}

// CustomAgents 返回当前注册的自定义子智能体快照。
func CustomAgents() []Definition {
	customMu.RLock()
	defer customMu.RUnlock()
	out := make([]Definition, len(customAgents))
	copy(out, customAgents)
	return out
}

// AllAgents 内置 + 自定义（设置页 / 会话切换 / 命令面板的完整可选集）。
func AllAgents() []Definition {
	return append(DefaultAgents(), CustomAgents()...)
}
