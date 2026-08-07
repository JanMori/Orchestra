package service

import (
	"context"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// HasCrewLeaderNoActionEvaluationForTask reports whether this exact task
// already recorded a crew leader no_action evaluation.
func HasCrewLeaderNoActionEvaluationForTask(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) (bool, error) {
	if q == nil || !task.ID.Valid || !task.IssueID.Valid || !task.AgentID.Valid {
		return false, nil
	}
	return q.HasCrewLeaderNoActionEvaluationForTask(ctx, db.HasCrewLeaderNoActionEvaluationForTaskParams{
		IssueID: task.IssueID,
		AgentID: task.AgentID,
		TaskID:  util.UUIDToString(task.ID),
	})
}
