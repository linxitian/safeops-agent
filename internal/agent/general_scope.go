package agent

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"safeops-agent/internal/llm"
	"safeops-agent/internal/task"
)

const (
	safeOpsLabReadRoot      = "/var/lib/safeops/lab"
	maxRuntimeGuardFeedback = 4
)

var requestAbsolutePath = regexp.MustCompile("/[^\\s\\\"'`，。；;！？!?]+")

type generalReadScope struct {
	ResourceType    string
	AuthorizedPaths []string
	ExcludedPaths   []string
	Source          string
}

type readScopeViolation struct {
	Code          string
	Summary       string
	Tool          string
	AttemptedPath string
}

func deriveGeneralReadScope(request string, selectedResources []string) *generalReadScope {
	paths, excluded := requestPathScopes(request)
	source := "request"
	if len(paths) == 0 && isAmbiguousResourceFollowup(request) && !mentionsExplicitNonFileTopic(request) {
		paths = normalizedUniquePaths(selectedResources)
		source = "session.selected_resources"
	}
	if len(paths) == 0 {
		return nil
	}
	return &generalReadScope{ResourceType: "file", AuthorizedPaths: paths, ExcludedPaths: excluded, Source: source}
}

func requestScopePaths(request string) []string {
	paths, _ := requestPathScopes(request)
	return paths
}

func requestPathScopes(request string) ([]string, []string) {
	paths := make([]string, 0, 4)
	excluded := make([]string, 0, 2)
	lower := strings.ToLower(request)
	for _, alias := range []string{"safeops lab", "safeops 实验区", "safeops实验区"} {
		if index := strings.Index(lower, alias); index >= 0 {
			if readScopeMentionNegated(lower, index) {
				excluded = append(excluded, safeOpsLabReadRoot)
			} else {
				paths = append(paths, safeOpsLabReadRoot)
			}
			break
		}
	}
	for _, match := range requestAbsolutePath.FindAllStringIndex(request, -1) {
		if readScopeMentionNegated(request, match[0]) {
			excluded = append(excluded, request[match[0]:match[1]])
		} else {
			paths = append(paths, request[match[0]:match[1]])
		}
	}
	return normalizedUniquePaths(paths), normalizedUniquePaths(excluded)
}

func readScopeMentionNegated(request string, mentionStart int) bool {
	prefix := strings.ToLower(request[:mentionStart])
	if separator := strings.LastIndexAny(prefix, ",.!?，。；;！？\n"); separator >= 0 {
		prefix = prefix[separator+1:]
	}
	positiveClauseEnd := -1
	positiveMarkers := []string{
		" and read ", " and inspect ", " and check ", " and access ", " and do read ", " and do inspect ", " and do check ", " and do access ",
		" but read ", " but inspect ", " but check ", " but access ", " but instead read ", " but instead inspect ", " but instead check ", " but instead access ",
		": read ", ": inspect ", ": check ", ": access ",
	}
	for _, connector := range []string{"并", "再", "然后", "但", "但是", "只", "改为", "而是", "：", "： "} {
		for _, verb := range []string{"看", "查", "检查", "读取", "访问"} {
			positiveMarkers = append(positiveMarkers, connector+verb)
		}
	}
	for _, marker := range positiveMarkers {
		if index := strings.LastIndex(prefix, marker); index >= 0 && index+len(marker) > positiveClauseEnd {
			positiveClauseEnd = index + len(marker)
		}
	}
	if positiveClauseEnd >= 0 {
		prefix = prefix[positiveClauseEnd:]
	}
	negativeMarkers := []string{"不包括", "排除", "忽略", "exclude", "except", "skip", "ignore"}
	for _, negation := range []string{"不要", "禁止", "别", "不"} {
		for _, verb := range []string{"看", "查", "检查", "读取", "访问"} {
			negativeMarkers = append(negativeMarkers, negation+verb)
		}
	}
	for _, negation := range []string{"do not", "don't", "must not", "never"} {
		for _, verb := range []string{"read", "inspect", "check", "access"} {
			negativeMarkers = append(negativeMarkers, negation+" "+verb)
		}
	}
	for _, marker := range negativeMarkers {
		if strings.Contains(prefix, marker) {
			return true
		}
	}
	return false
}

func isAmbiguousResourceFollowup(request string) bool {
	lower := strings.ToLower(strings.TrimSpace(request))
	for _, marker := range []string{
		"这些", "上述", "前面", "刚才", "其中", "第一个", "第二个", "第三个", "第四个", "第五个", "最后一个", "哪个", "哪些", "该文件", "这个文件", "该资源", "处理它", "检查它", "查看它", "它呢", "它的",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, marker := range []string{"these files", "those files", "selected file", "selected resource", "previous file", "this file", "that file", "this one", "that one", "which file", "which one", "which should", "them", "it"} {
		if containsASCIIPhrase(lower, marker) {
			return true
		}
	}
	return false
}

func mentionsExplicitNonFileTopic(request string) bool {
	lower := strings.ToLower(request)
	for _, marker := range []string{"内存", "负载", "服务", "端口", "进程", "网络"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, marker := range []string{"cpu", "journal", "systemd", "service", "port", "process", "network", "memory", "load average"} {
		if containsASCIIPhrase(lower, marker) {
			return true
		}
	}
	return false
}

func containsASCIIPhrase(value, phrase string) bool {
	for offset := 0; offset <= len(value)-len(phrase); {
		index := strings.Index(value[offset:], phrase)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(phrase)
		beforeOK := start == 0 || !isASCIIWordByte(value[start-1])
		afterOK := end == len(value) || !isASCIIWordByte(value[end])
		if beforeOK && afterOK {
			return true
		}
		offset = start + 1
	}
	return false
}

func isASCIIWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || value == '_'
}

func normalizedUniquePaths(values []string) []string {
	seen := map[string]bool{}
	paths := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimRight(strings.TrimSpace(value), ".,!?，。；;！？：:)]}）】》>`")
		if !filepath.IsAbs(value) {
			continue
		}
		value = filepath.Clean(value)
		if seen[value] {
			continue
		}
		seen[value] = true
		paths = append(paths, value)
	}
	sort.Strings(paths)
	return paths
}

func validateGeneralReadScope(scope *generalReadScope, decision llm.Decision) *readScopeViolation {
	if scope == nil || decision.Kind != llm.DecisionTool {
		return nil
	}
	tool := decision.ServerID + "/" + decision.Tool
	if !scopedPathReadTool(tool) {
		return &readScopeViolation{
			Code:    "REQUEST_READ_SCOPE_TOOL_MISMATCH",
			Summary: "文件范围任务只能选择文件元数据、文件系统容量或磁盘压力的路径受限工具",
			Tool:    tool,
		}
	}
	pathValue, hasPath := decision.Arguments["path"]
	if !hasPath {
		return &readScopeViolation{
			Code:    "REQUEST_READ_SCOPE_PATH_REQUIRED",
			Summary: "文件范围任务只能选择携带受权 path 的只读工具",
			Tool:    tool,
		}
	}
	path, ok := pathValue.(string)
	if !ok || !filepath.IsAbs(path) {
		return &readScopeViolation{
			Code:          "REQUEST_READ_SCOPE_PATH_INVALID",
			Summary:       "文件范围任务要求绝对 path，且该 path 必须位于本次请求的受权范围内",
			Tool:          tool,
			AttemptedPath: strings.TrimSpace(path),
		}
	}
	path = filepath.Clean(path)
	for _, excluded := range scope.ExcludedPaths {
		if pathWithinScope(excluded, path) || (scopedPathReadToolTraverses(tool) && pathWithinScope(path, excluded)) {
			return &readScopeViolation{
				Code:          "REQUEST_READ_SCOPE_EXCLUDED",
				Summary:       "工具 path 位于操作员明确排除的路径内，或会遍历到该排除路径",
				Tool:          tool,
				AttemptedPath: path,
			}
		}
	}
	for _, authorized := range scope.AuthorizedPaths {
		if pathWithinScope(authorized, path) {
			return nil
		}
	}
	return &readScopeViolation{
		Code:          "REQUEST_READ_SCOPE_MISMATCH",
		Summary:       "工具 path 超出操作员本次请求或会话已选资源的受权范围",
		Tool:          tool,
		AttemptedPath: path,
	}
}

func scopedPathReadToolTraverses(tool string) bool {
	switch tool {
	case "file/file.list_directory", "file/file.find_large", "config/config.snapshot", "config/config.diff_snapshot":
		return true
	default:
		return false
	}
}

func scopedPathReadTool(tool string) bool {
	switch tool {
	case "file/file.stat", "file/file.list_directory", "file/file.sha256", "file/file.find_large",
		"config/config.get_metadata", "config/config.snapshot", "config/config.diff_snapshot",
		"system/system.get_disk_usage", "diagnostic/diagnostic.disk_pressure":
		return true
	default:
		return false
	}
}

func pathWithinScope(authorized, candidate string) bool {
	authorized = filepath.Clean(authorized)
	candidate = filepath.Clean(candidate)
	if authorized == candidate {
		return true
	}
	relative, err := filepath.Rel(authorized, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func plannerReadScope(scope *generalReadScope) *llm.ReadScope {
	if scope == nil {
		return nil
	}
	return &llm.ReadScope{
		ResourceType:    scope.ResourceType,
		AuthorizedPaths: append([]string(nil), scope.AuthorizedPaths...),
		ExcludedPaths:   append([]string(nil), scope.ExcludedPaths...),
		Source:          scope.Source,
	}
}

func plannerGuardFeedback(values []task.RuntimeGuardFeedback) []llm.GuardFeedback {
	out := make([]llm.GuardFeedback, 0, len(values))
	for _, value := range values {
		out = append(out, llm.GuardFeedback{
			Code:            value.Code,
			Summary:         value.Summary,
			Tool:            value.Tool,
			AttemptedPath:   value.AttemptedPath,
			AuthorizedPaths: append([]string(nil), value.AuthorizedPaths...),
			ExcludedPaths:   append([]string(nil), value.ExcludedPaths...),
		})
	}
	return out
}

func appendRuntimeGuardFeedback(checkpoint *task.RuntimeCheckpoint, scope *generalReadScope, violation *readScopeViolation) {
	feedback := task.RuntimeGuardFeedback{
		Code:          violation.Code,
		Summary:       violation.Summary,
		Tool:          violation.Tool,
		AttemptedPath: violation.AttemptedPath,
	}
	if scope != nil {
		feedback.AuthorizedPaths = append([]string(nil), scope.AuthorizedPaths...)
		feedback.ExcludedPaths = append([]string(nil), scope.ExcludedPaths...)
	}
	checkpoint.GuardFeedback = append(checkpoint.GuardFeedback, feedback)
	if len(checkpoint.GuardFeedback) > maxRuntimeGuardFeedback {
		checkpoint.GuardFeedback = append([]task.RuntimeGuardFeedback(nil), checkpoint.GuardFeedback[len(checkpoint.GuardFeedback)-maxRuntimeGuardFeedback:]...)
	}
}
