package service

import "regexp"

// artifactClaimRe 匹配「已产出文件」类声明：完成标记 + 文件扩展名同句相邻出现
// （中间 ≤60 字符，句号/换行截断）。单独出现动作词或文件名都不算。
var artifactClaimRe = regexp.MustCompile(
	`(?i)((已|已经|成功)[^。\n]{0,12}(创建|生成|保存|写入|写出|导出|输出|制作)|(created|generated|saved|wrote|exported))` +
		`[^。\n]{0,60}\.(pptx|docx|xlsx|pdf|md|txt|csv|html|htm|zip|png|jpe?g|json|mp4|mp3)`)

// claimsArtifact 判断回复是否包含「已产出文件」类声明；仅在 run 零工具调用时用于幻觉告警。
func claimsArtifact(content string) bool {
	return content != "" && artifactClaimRe.MatchString(content)
}
