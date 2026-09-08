package pkg

import (
	"regexp"
	"strings"
)

// nonWordRe 用非字母数字作分隔切词；保证 token 内不含引号等 FTS5 语法字符（结构性防注入）。
var nonWordRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// MinGramLen trigram tokenizer 的最小可查长度；短于它的 token 匹配不到任何 3-gram。
const MinGramLen = 3

// MatchTokens 自由文本切词：按非字母数字分隔，丢弃不足 3 字的 token，去重保序。
// 供按列构造 MATCH 表达式的调用方使用。
func MatchTokens(query string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, tok := range nonWordRe.Split(strings.TrimSpace(query), -1) {
		if len([]rune(tok)) < MinGramLen || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// BuildMatchQuery 把自由文本转成 FTS5 MATCH 表达式：token 双引号包裹后 OR 连接。
// 知识库与记忆共用同一套切词 / OR 口径，避免两处漂移。
// 返回空串表示所有 token 都过短，调用方应走 LIKE 兜底而非静默无结果。
func BuildMatchQuery(query string) string {
	toks := MatchTokens(query)
	parts := make([]string, 0, len(toks))
	for _, t := range toks {
		parts = append(parts, `"`+t+`"`)
	}
	return strings.Join(parts, " OR ")
}

// SplitTokens 自由文本切词：按非字母/数字分隔，不过滤长度、不去重。
//
// 与 MatchTokens 的区别：保留 2 字中文短词（trigram MATCH 用不上它们，
// 但 LIKE 兜底与子串召回场景正需要）；调用方按需去重。
func SplitTokens(query string) []string {
	return nonWordRe.Split(strings.TrimSpace(query), -1)
}

// EscapeLike 转义 LIKE 通配符（配合 ESCAPE '\' 使用），防用户输入里的 %/_ 撑大匹配面。
func EscapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	return strings.ReplaceAll(s, `_`, `\_`)
}
