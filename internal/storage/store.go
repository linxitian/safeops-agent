package storage

import (
	"context"
	"time"

	"safeops-agent/internal/session"
	"safeops-agent/internal/task"
)

type Store interface {
	SaveSession(context.Context, session.Session) error
	UpdateSession(context.Context, string, func(*session.Session) error) (session.Session, error)
	GetSession(context.Context, string) (session.Session, error)
	ListSessions(context.Context) ([]session.Session, error)
	SaveTask(context.Context, task.Task) error
	ClaimTask(context.Context, string, string, string, time.Duration) (task.Task, error)
	ReleaseTask(context.Context, string, string, uint64) (task.Task, error)
	GetTask(context.Context, string) (task.Task, error)
	ListTasks(context.Context) ([]task.Task, error)
}
