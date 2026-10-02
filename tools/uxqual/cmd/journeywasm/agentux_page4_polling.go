package main

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func selectedAgentTaskCanRefresh(taskID string, tasks []productui.AgentTask) bool {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return false
	}
	for _, task := range tasks {
		if task.ID == taskID {
			return true
		}
	}
	return false
}

func agentTaskNeedsPolling(task productui.AgentTask) bool {
	return !agentTaskFinal(task)
}

func pollAgentTask(ctx context.Context, interval time.Duration, taskID string, get func(context.Context, string) (productui.AgentTask, error), update func(productui.AgentTask)) {
	if ctx == nil || interval <= 0 || strings.TrimSpace(taskID) == "" || get == nil || update == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			task, err := get(ctx, taskID)
			if err != nil {
				continue
			}
			update(task)
			if !agentTaskNeedsPolling(task) {
				return
			}
		}
	}
}
