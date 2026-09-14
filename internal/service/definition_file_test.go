package service

// 定义文件（命令 / 子智能体）测试。
//
// 覆盖三条链路：单文件解析与拒绝规则、目录级加载的容错、模型与推理强度的映射与校验。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/runtime"
)

// TestDefinitionFileParsing 单文件解析：命令模板、子智能体映射、frontmatter 边界与拒绝规则。
func TestDefinitionFileParsing(t *testing.T) {
	t.Run("命令文件", func(t *testing.T) {
		ok, valid := parseCommandFile([]byte("---\ndescription: 审查改动\nargument-hint: \"[范围]\"\n---\n请审查 $ARGUMENTS 的改动"), "review")
		require.True(t, valid)
		assert.Equal(t, "review", ok.Name)
		assert.Equal(t, "审查改动", ok.Desc)
		assert.Equal(t, "[范围]", ok.Args)
		assert.Equal(t, "custom", ok.Group)
		assert.True(t, ok.ClientOnly)
		assert.Contains(t, ok.Prompt, "$ARGUMENTS")
		// 来源由加载器按目录打标（parseCommandFile 只管单个文件的内容）
		assert.Empty(t, ok.Source)

		// 空正文：没有可发送的提示词，跳过而不是产出一个空命令
		if _, v := parseCommandFile([]byte("---\ndescription: 空的\n---\n\n"), "empty"); v {
			t.Fatal("empty prompt must be rejected")
		}
		// 未接入执行链的字段不影响解析（只告警），命令仍然可用
		if _, v := parseCommandFile([]byte("---\nmodel: gpt-4o\nallowed-tools: exec\n---\nbody"), "with-extra"); !v {
			t.Fatal("unsupported fields must not drop the command")
		}
	})

	t.Run("子智能体文件", func(t *testing.T) {
		builtin := map[string]struct{}{"default": {}, "explore": {}}

		def, ok := parseAgentFile([]byte(
			"---\nname: reviewer\ndescription: 代码审查员\ntools: file_read, file_grep\ndisallowedTools: file_write\nmaxTurns: 8\n---\n你是审查员。",
		), "reviewer.md", builtin)
		require.True(t, ok)
		assert.Equal(t, "reviewer", def.Name)
		assert.Equal(t, []string{"file_read", "file_grep"}, def.Tools.Allow)
		assert.Equal(t, []string{"file_write"}, def.Tools.Deny)
		assert.Equal(t, 8, def.Budget.MaxTurns)
		assert.False(t, def.Memory.Enabled, "子智能体不写长期记忆")
		assert.Contains(t, def.Persona, "你是审查员")

		// tools: * → 不设白名单（等价「全部工具」）
		all, ok := parseAgentFile([]byte("---\nname: any\ndescription: d\ntools: \"*\"\n---\nbody"), "any.md", builtin)
		require.True(t, ok)
		assert.Nil(t, all.Tools.Allow)

		// 缺 description / 名非法 / 撞内置名 → 拒绝
		for _, in := range []string{
			"---\nname: no-desc\n---\nbody",
			"---\nname: Bad_Name\ndescription: d\n---\nbody",
			"---\nname: explore\ndescription: 想覆盖内置\n---\nbody",
		} {
			if _, v := parseAgentFile([]byte(in), "x.md", builtin); v {
				t.Fatalf("must be rejected: %s", in)
			}
		}
	})

	t.Run("frontmatter 边界", func(t *testing.T) {
		// 无 frontmatter：整份内容即正文
		plain := "just a prompt"
		fm := splitFrontMatter(plain)
		assert.Equal(t, plain, fm.body)
		assert.Empty(t, fm.get("description"))

		// 列表 / 整数 / 布尔解析，缺省值可指定
		typed := splitFrontMatter("---\ntools: [file_read, file_grep]\nmaxTurns: 12\ninjectAgentsMd: false\n---\nb")
		assert.Equal(t, []string{"file_read", "file_grep"}, typed.list("tools"))
		assert.Equal(t, 12, typed.intValue("maxTurns", 0))
		assert.False(t, typed.boolean("injectAgentsMd", true))
		assert.Equal(t, 7, typed.intValue("missing", 7))

		// 未闭合的 --- 块不是 frontmatter，整体当正文处理（否则会把正文吃掉）
		broken := "---\ndescription: broken"
		bfm := splitFrontMatter(broken)
		assert.Equal(t, broken, bfm.body)
		assert.Empty(t, bfm.get("description"))
	})
}

// TestLoadDefinitionFiles 目录级加载：坏文件跳过、好文件进入注册表（不因一个文件写坏而整体失败）。
func TestLoadDefinitionFiles(t *testing.T) {
	home := t.TempDir()
	cmdDir := filepath.Join(home, "commands")
	agentDir := filepath.Join(home, "agents")
	require.NoError(t, os.MkdirAll(cmdDir, 0o755))
	require.NoError(t, os.MkdirAll(agentDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "review.md"),
		[]byte("---\ndescription: 审查\n---\n审查 $1 与 $2"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "Bad Name.md"), []byte("---\n---\nbody"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "notes.txt"), []byte("ignored"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "helper.md"),
		[]byte("---\nname: helper\ndescription: 助手\n---\nbody"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "default.md"),
		[]byte("---\nname: default\ndescription: 撞内置\n---\nbody"), 0o644))

	cmds := LoadCommandFiles(home)
	require.Len(t, cmds, 1, "只有合法命名的 .md 命令文件应被加载")
	assert.Equal(t, "review", cmds[0].Name)
	assert.Equal(t, domain.CommandSourceFile, cmds[0].Source)

	agents := LoadAgentFiles(home)
	require.Len(t, agents, 1, "撞内置名的定义文件应被拒绝")
	assert.Equal(t, "helper", agents[0].Name)
	assert.IsType(t, harness.Definition{}, agents[0])

	// 工作区级命令：<ws>/.workbaby/commands，来源标记与用户级区分
	ws := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ws, runtime.SandboxDirName, "commands"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, runtime.SandboxDirName, "commands", "deploy.md"),
		[]byte("---\ndescription: 本项目发布流程\n---\n按项目规范发布"), 0o644))
	wsCmds := LoadWorkspaceCommandFiles(ws)
	require.Len(t, wsCmds, 1)
	assert.Equal(t, "deploy", wsCmds[0].Name)
	assert.Equal(t, domain.CommandSourceWorkspace, wsCmds[0].Source)

	// 目录不存在：返回空而不是报错（首次启动的正常状态）
	assert.Nil(t, LoadCommandFiles(filepath.Join(home, "nope")))
	assert.Nil(t, LoadAgentFiles(filepath.Join(home, "nope")))
	assert.Nil(t, LoadCommandFiles(""))
	// 未绑定工作区：工作区级命令不加载（与工作区技能同一约定）
	assert.Nil(t, LoadWorkspaceCommandFiles(""))
	assert.Equal(t, "", WorkspaceCommandDir(""))
}

// TestAgentModelThinking 模型与推理强度的映射和校验。
//
// 两个入口同一套规则：定义文件（parseAgentFile）与设置页（validateProfileReq）。
// 规则的核心是「推理强度脱离模型就没有意义」——只配强度不配模型会让用户当次的
// 推理档位选择静默失效，所以两处都直接拒绝而不是存下来。
func TestAgentModelThinking(t *testing.T) {
	builtin := map[string]struct{}{"default": {}}

	t.Run("定义文件入口", func(t *testing.T) {
		// model + thoughtLevel：两者都生效
		def, ok := parseAgentFile([]byte(
			"---\nname: heavy\ndescription: 重活\ntools: \"*\"\nmodel: gpt-5\nthoughtLevel: HIGH\n---\nbody",
		), "heavy.md", builtin)
		require.True(t, ok)
		assert.Equal(t, "gpt-5", def.Model)
		assert.Equal(t, "high", def.Thinking, "推理强度应归一为小写")
		assert.Equal(t, "gpt-5", def.EffectiveModel("session-model"))
		// 未指定模型：跟随会话
		assert.Equal(t, "session-model", harness.Definition{}.EffectiveModel("session-model"))

		// thoughtLevel 未配 model：不生效（否则会让用户当次的推理档位选择失效）
		only, ok := parseAgentFile([]byte("---\nname: level-only\ndescription: d\nthoughtLevel: high\n---\nbody"), "l.md", builtin)
		require.True(t, ok)
		assert.Empty(t, only.Thinking)

		// 非法档位：丢弃而不是写入
		bad, ok := parseAgentFile([]byte("---\nname: bad\ndescription: d\nmodel: m\nthoughtLevel: ultra\n---\nbody"), "b.md", builtin)
		require.True(t, ok)
		assert.Empty(t, bad.Thinking)

		// inherit 与不写等价
		inherit, ok := parseAgentFile([]byte("---\nname: inh\ndescription: d\nmodel: inherit\n---\nbody"), "i.md", builtin)
		require.True(t, ok)
		assert.Empty(t, inherit.Model)
	})

	t.Run("设置页入口", func(t *testing.T) {
		base := domain.AgentProfileREQ{Name: "reviewer", Description: "审查"}

		_, err := validateProfileReq(&domain.AgentProfileREQ{Name: base.Name, Description: base.Description, Thinking: "high"})
		require.Error(t, err, "只给推理强度不给模型必须被拒")

		_, err = validateProfileReq(&domain.AgentProfileREQ{Name: base.Name, Description: base.Description, Model: "gpt-5", Thinking: "ultra"})
		require.Error(t, err, "非法档位必须被拒")

		row, err := validateProfileReq(&domain.AgentProfileREQ{
			Name: base.Name, Description: base.Description, Model: "gpt-5", Thinking: "HIGH",
		})
		require.NoError(t, err)
		assert.Equal(t, "gpt-5", row.Model)
		assert.Equal(t, "high", row.Thinking)

		def := profileToDefinition(row)
		assert.Equal(t, "gpt-5", def.Model)
		assert.Equal(t, "high", def.Thinking)
	})
}
