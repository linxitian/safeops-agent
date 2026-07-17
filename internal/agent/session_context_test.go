package agent

import (
	"strings"
	"testing"
	"unicode/utf8"

	"safeops-agent/internal/session"
)

func TestBuildPlannerSessionContextIsBoundedOrderedAndRedacted(t *testing.T) {
	messages := []session.Message{
		{Role: session.RoleSystem, Content: "internal system message"},
		{Role: session.RoleUser, Content: "检查这些文件，API_KEY=should-not-leak"},
		{Role: session.RoleAssistant, Content: "找到了三个文件"},
	}
	for index := 0; index < 8; index++ {
		messages = append(messages, session.Message{Role: session.RoleUser, Content: strings.Repeat("界", 1000)})
	}
	messages = append(messages, session.Message{Role: session.RoleUser, Content: "当前问题不应重复", TaskID: "task_current"})
	resources := make([]string, 0, 55)
	for index := 0; index < 55; index++ {
		resources = append(resources, "/var/lib/safeops/lab/file-"+strings.Repeat("x", 400)+string(rune('a'+index)))
	}
	value := session.Session{
		Summary:           "Bearer abcdefghijklmnopqrstuvwxyz",
		Messages:          messages,
		SelectedResources: resources,
	}

	context := buildPlannerSessionContext(value, "task_current")
	if context == nil {
		t.Fatal("bounded context was omitted")
	}
	if len(context.RecentMessages) == 0 || len(context.RecentMessages) > maxPlannerContextMessages {
		t.Fatalf("recent message count is outside bounds: %d", len(context.RecentMessages))
	}
	total := 0
	for _, message := range context.RecentMessages {
		total += len(message.Content)
		if len(message.Content) > maxPlannerMessageBytes || !utf8.ValidString(message.Content) {
			t.Fatalf("invalid bounded message: bytes=%d valid=%v", len(message.Content), utf8.ValidString(message.Content))
		}
		if strings.Contains(message.Content, "当前问题") || strings.Contains(message.Content, "should-not-leak") {
			t.Fatalf("excluded or secret content leaked: %q", message.Content)
		}
	}
	if total > maxPlannerMessagesBytes {
		t.Fatalf("message context exceeds byte bound: %d", total)
	}
	if context.Summary != "Bearer [REDACTED]" {
		t.Fatalf("summary secret was not redacted: %q", context.Summary)
	}
	if len(context.SelectedResources) > maxPlannerSelectedResources {
		t.Fatalf("selected resource count exceeds bound: %d", len(context.SelectedResources))
	}
	resourceBytes := 0
	for _, resource := range context.SelectedResources {
		resourceBytes += len(resource)
		if len(resource) > maxPlannerSelectedResourceBytes || !utf8.ValidString(resource) {
			t.Fatalf("invalid bounded resource: bytes=%d valid=%v", len(resource), utf8.ValidString(resource))
		}
	}
	if resourceBytes > maxPlannerSelectedResourcesBytes {
		t.Fatalf("selected resources exceed byte bound: %d", resourceBytes)
	}
}

func TestRedactPlannerSecretsCoversConfiguredAndBearerForms(t *testing.T) {
	input := "API_KEY=should-not-leak password: also-secret Bearer abcdefghijklmnop sk-abcdefghijklmnop"
	redacted := redactPlannerSecrets(input)
	for _, secret := range []string{"should-not-leak", "also-secret", "abcdefghijklmnop"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("secret %q leaked through redaction: %q", secret, redacted)
		}
	}
}

func TestBuildPlannerSessionContextPreservesVisibleTurnOrder(t *testing.T) {
	value := session.Session{Messages: []session.Message{
		{Role: session.RoleUser, Content: "first"},
		{Role: session.RoleAssistant, Content: "second"},
		{Role: session.RoleUser, Content: "current", TaskID: "task_current"},
	}}
	context := buildPlannerSessionContext(value, "task_current")
	if context == nil || len(context.RecentMessages) != 2 || context.RecentMessages[0].Content != "first" || context.RecentMessages[1].Content != "second" {
		t.Fatalf("visible history order changed: %+v", context)
	}
}
