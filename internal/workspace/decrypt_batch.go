package workspace

import "time.haomen/binggan/v2/internal/tasks"

func (a *App) RetryTask(taskID string, ids []string) (tasks.View, error) {
	if taskID == "" || len(ids) < 1 || len(ids) > 100 {
		return tasks.View{}, invalidTask("请选择要重试的视频")
	}
	for _, id := range ids {
		if id == "" {
			return tasks.View{}, invalidTask("视频 ID 不能为空")
		}
	}
	return a.Tasks.Retry(taskID, ids)
}
