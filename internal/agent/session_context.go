package agent

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"safeops-agent/internal/llm"
	"safeops-agent/internal/session"
)

const (
	maxPlannerContextMessages        = 6
	maxPlannerMessageBytes           = 2 << 10
	maxPlannerMessagesBytes          = 8 << 10
	maxPlannerSummaryBytes           = 2 << 10
	maxPlannerSelectedResources      = 50
	maxPlannerSelectedResourceBytes  = 2 << 10
	maxPlannerSelectedResourcesBytes = 16 << 10
)

var plannerSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)((?:api[_-]?key|access[_-]?token|secret|password|passwd|密码)\s*[:=]\s*)\S+`),
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`(?i)\bsk-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`\b[A-Za-z0-9_-]{32,}\.[A-Za-z0-9_-]{8,}\b`),
}

func buildPlannerSessionContext(value session.Session, currentTaskID string) *llm.SessionContext {
	context := &llm.SessionContext{Summary: boundedPlannerText(value.Summary, maxPlannerSummaryBytes)}

	messages := make([]llm.SessionMessage, 0, maxPlannerContextMessages)
	remaining := maxPlannerMessagesBytes
	for index := len(value.Messages) - 1; index >= 0 && len(messages) < maxPlannerContextMessages && remaining > 0; index-- {
		message := value.Messages[index]
		if message.TaskID == currentTaskID || message.Role != session.RoleUser && message.Role != session.RoleAssistant {
			continue
		}
		content := redactPlannerSecrets(strings.TrimSpace(message.Content))
		content = truncateUTF8(content, min(maxPlannerMessageBytes, remaining))
		if content == "" {
			continue
		}
		messages = append(messages, llm.SessionMessage{Role: string(message.Role), Content: content})
		remaining -= len(content)
	}
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	context.RecentMessages = messages

	remaining = maxPlannerSelectedResourcesBytes
	seen := map[string]bool{}
	for _, raw := range value.SelectedResources {
		if len(context.SelectedResources) == maxPlannerSelectedResources || remaining == 0 {
			break
		}
		resource := truncateUTF8(redactPlannerSecrets(strings.TrimSpace(raw)), min(maxPlannerSelectedResourceBytes, remaining))
		if resource == "" || seen[resource] {
			continue
		}
		seen[resource] = true
		context.SelectedResources = append(context.SelectedResources, resource)
		remaining -= len(resource)
	}

	if context.Summary == "" && len(context.RecentMessages) == 0 && len(context.SelectedResources) == 0 {
		return nil
	}
	return context
}

func boundedPlannerText(value string, limit int) string {
	return truncateUTF8(redactPlannerSecrets(strings.TrimSpace(value)), limit)
}

func redactPlannerSecrets(value string) string {
	for index, pattern := range plannerSecretPatterns {
		switch index {
		case 0:
			value = pattern.ReplaceAllString(value, `${1}[REDACTED]`)
		case 1:
			value = pattern.ReplaceAllString(value, `Bearer [REDACTED]`)
		default:
			value = pattern.ReplaceAllString(value, `[REDACTED]`)
		}
	}
	return value
}

func truncateUTF8(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
