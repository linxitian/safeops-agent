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

var requestAbsolutePath = regexp.MustCompile(`/[^\s"'，。；;]+`)

type generalReadScope struct {
	ResourceType    string
	AuthorizedPaths []string
	Source          string
}

type readScopeViolation struct {
	Code          string
	Summary       string
	Tool          string
	AttemptedPath string
}

func deriveGeneralReadScope(request string, selectedResources []string) *generalReadScope {
	paths := requestScopePaths(request)
	source := "request"
	if len(paths) == 0 && isAmbiguousResourceFollowup(request) && !mentionsExplicitNonFileTopic(request) {
		paths = normalizedUniquePaths(selectedResources)
		source = "session.selected_resources"
	}
	if len(paths) == 0 {
		return nil
	}
	return &generalReadScope{ResourceType: "file", AuthorizedPaths: paths, Source: source}
}

func requestScopePaths(request string) []string {
	paths := make([]string, 0, 4)
	lower := strings.ToLower(request)
	for _, alias := range []string{"safeops lab", "safeops 实验区", "safeops实验区"} {
		if index := strings.Index(lower, alias); index >= 0 && !readScopeMentionNegated(lower, index) {
			paths = append(paths, safeOpsLabReadRoot)
			break
		}
	}
	for _, match := range requestAbsolutePath.FindAllStringIndex(request, -1) {
		if !readScopeMentionNegated(request, match[0]) {
			paths = append(paths, request[match[0]:match[1]])
		}
	}
	return normalizedUniquePaths(paths)
}

func readScopeMentionNegated(request string, mentionStart int) bool {
	prefix := strings.ToLower(request[:mentionStart])
	if separator := strings.LastIndexAny(prefix, "，。；;\n"); separator >= 0 {
		prefix = prefix[separator+1:]
	}
	for _, marker := range []string{
		"不要看", "不要查", "不要读取", "不要访问", "禁止查看", "禁止读取", "别看", "别查", "不包括", "排除",
		"do not inspect", "do not read", "do not access", "don't inspect", "don't read", "don't access", "exclude", "except",
	} {
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
		"these files", "those files", "selected file", "selected resource", "previous file", "which file", "which one", "which should", "them",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func mentionsExplicitNonFileTopic(request string) bool {
	lower := strings.ToLower(request)
	for _, marker := range []string{
		"cpu", "内存", "负载", "服务", "端口", "进程", "网络", "journal", "systemd", "service", "port", "process", "network", "memory", "load average",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func normalizedUniquePaths(values []string) []string {
	seen := map[string]bool{}
	paths := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimRight(strings.TrimSpace(value), ".,，。；;：:)]}）】》>")
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
	pathValue, hasPath := decision.Arguments["path"]
	if !hasPath {
		if decision.ServerID == "file" && decision.Tool == "file.list_roots" {
			return nil
		}
		return &readScopeViolation{
			Code:    "REQUEST_READ_SCOPE_PATH_REQUIRED",
			Summary: "文件范围任务只能选择携带受权 path 的只读工具；file.list_roots 仅可用于发现根目录",
			Tool:    decision.ServerID + "/" + decision.Tool,
		}
	}
	path, ok := pathValue.(string)
	if !ok || !filepath.IsAbs(path) {
		return &readScopeViolation{
			Code:          "REQUEST_READ_SCOPE_PATH_INVALID",
			Summary:       "文件范围任务要求绝对 path，且该 path 必须位于本次请求的受权范围内",
			Tool:          decision.ServerID + "/" + decision.Tool,
			AttemptedPath: strings.TrimSpace(path),
		}
	}
	path = filepath.Clean(path)
	for _, authorized := range scope.AuthorizedPaths {
		if pathWithinScope(authorized, path) {
			return nil
		}
	}
	return &readScopeViolation{
		Code:          "REQUEST_READ_SCOPE_MISMATCH",
		Summary:       "工具 path 超出操作员本次请求或会话已选资源的受权范围",
		Tool:          decision.ServerID + "/" + decision.Tool,
		AttemptedPath: path,
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
	}
	checkpoint.GuardFeedback = append(checkpoint.GuardFeedback, feedback)
	if len(checkpoint.GuardFeedback) > maxRuntimeGuardFeedback {
		checkpoint.GuardFeedback = append([]task.RuntimeGuardFeedback(nil), checkpoint.GuardFeedback[len(checkpoint.GuardFeedback)-maxRuntimeGuardFeedback:]...)
	}
}
