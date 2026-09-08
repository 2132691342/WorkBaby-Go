package harness

// 内置 Agent 定义表（v1 硬编码，UI 可视化配置留 P2，YAGNI）。
//
// chat / workflow / cron 以 name 引用同一个 Definition：一次定义、随处运行，
// 替换各调用点硬编码的模型/工具/记忆/预算参数。未来扩展 = 往表里加一条。

// DefaultAgents 返回全部内置 Agent。
func DefaultAgents() []Definition {
	return []Definition{
		{
			Name:        "default",
			Description: "通用全能助手（默认）：记忆开启、可执行工具",
			Persona:     personaDefault,
			Tools: ToolPolicy{
				// 工具白名单显式收敛到核心集：functools 纯函数（30+ 个）默认不在此，
				// 用户可在设置页手动开启；命令级裁决类（exec / skill / delegate / workflow）
				// 由 runner.gateTool 统一拦截危险命令，白名单仅控制「带不带」。
				Allow: []string{
					"file_*", "doc_reader", "archive_manager",
					"exec", "run_skill_script", "delegate_task", "run_workflow",
					"websearch", "webfetch", "http",
					"knowledge_search", "memory_write",
					"todo", "request_input",
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

// Agent 按名取内置 Agent；未知名回退 default（运行时按名引用不硬失败）。
func Agent(name string) Definition {
	for _, d := range DefaultAgents() {
		if d.Name == name {
			return d
		}
	}
	return DefaultAgents()[0]
}
