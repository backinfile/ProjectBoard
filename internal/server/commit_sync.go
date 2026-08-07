package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/projectboard/projectboard/internal/providers"
	"github.com/projectboard/projectboard/internal/security"
)

func (s *Server) syncProjectCommitsHuman(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.PathValue("id")
	if err := s.requireDeveloper(projectID, a); err != nil {
		writeError(w, err)
		return
	}
	result, err := s.syncProjectCommits(r.Context(), projectID, a)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) syncProjectCommits(ctx context.Context, projectID string, a actor) (map[string]any, error) {
	windowEnd := time.Now().UTC()
	var previous sql.NullString
	_ = s.store.DB.QueryRowContext(ctx, "SELECT last_synced_at FROM project_commit_sync_state WHERE project_id=?", projectID).Scan(&previous)
	windowStart := windowEnd.AddDate(0, -1, 0)
	if previous.Valid {
		if parsed, parseErr := time.Parse(time.RFC3339Nano, previous.String); parseErr == nil {
			windowStart = parsed
		}
	}
	var provider, authorizationID, repositoryID, branch string
	var installationID sql.NullString
	err := s.store.DB.QueryRow(`SELECT p.provider,p.id,g.repository_id,g.default_branch,p.installation_id FROM project_repository_grants g JOIN provider_authorizations p ON p.id=g.authorization_id WHERE g.project_id=? AND g.revoked_at IS NULL AND p.status='active' AND p.provider='github'`, projectID).Scan(&provider, &authorizationID, &repositoryID, &branch, &installationID)
	if err == sql.ErrNoRows {
		return nil, &domainError{409, "REPOSITORY_GRANT_REQUIRED", "Connect and approve a repository before syncing commits", nil}
	}
	if err != nil {
		return nil, err
	}
	var workItemCount int
	if err = s.store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_items WHERE project_id=?", projectID).Scan(&workItemCount); err != nil {
		return nil, err
	}
	if workItemCount == 0 {
		if err = s.updateCommitSyncTimestamp(ctx, projectID, windowEnd, a); err != nil {
			return nil, err
		}
		return map[string]any{"provider": provider, "repositoryId": repositoryID, "fetched": 0, "inserted": 0, "matchedWorkItems": 0, "commits": []providers.RecentCommit{}, "syncMode": "on_demand", "windowStart": windowStart.Format(time.RFC3339Nano), "windowEnd": windowEnd.Format(time.RFC3339Nano), "skipped": "no_work_items"}, nil
	}
	var commits []providers.RecentCommit
	if !installationID.Valid {
		return nil, fmt.Errorf("GitHub authorization has no installation ID")
	}
	commits, err = s.gitClient().RecentGitHubCommits(ctx, installationID.String, repositoryID, branch, windowStart.Format(time.RFC3339), windowEnd.Format(time.RFC3339))
	if err != nil {
		return nil, &domainError{502, "COMMIT_SYNC_FAILED", err.Error(), nil}
	}
	if len(commits) == 0 {
		if err = s.updateCommitSyncTimestamp(ctx, projectID, windowEnd, a); err != nil {
			return nil, err
		}
		return map[string]any{"provider": provider, "repositoryId": repositoryID, "fetched": 0, "inserted": 0, "matchedWorkItems": 0, "commits": commits, "syncMode": "on_demand", "windowStart": windowStart.Format(time.RFC3339Nano), "windowEnd": windowEnd.Format(time.RFC3339Nano), "skipped": "no_commits"}, nil
	}
	inserted, matches, err := s.recordRecentCommits(ctx, projectID, repositoryID, branch, commits, a)
	if err != nil {
		return nil, err
	}
	if err = s.updateCommitSyncTimestamp(ctx, projectID, windowEnd, a); err != nil {
		return nil, err
	}
	s.audit(a, "git.commits_synced", "repository", repositoryID, projectID, map[string]any{"provider": provider, "authorizationId": authorizationID, "fetched": len(commits), "inserted": inserted, "matchedWorkItems": matches, "windowStart": windowStart.Format(time.RFC3339Nano), "windowEnd": windowEnd.Format(time.RFC3339Nano)})
	return map[string]any{"provider": provider, "repositoryId": repositoryID, "fetched": len(commits), "inserted": inserted, "matchedWorkItems": matches, "commits": commits, "syncMode": "on_demand", "windowStart": windowStart.Format(time.RFC3339Nano), "windowEnd": windowEnd.Format(time.RFC3339Nano)}, nil
}

func (s *Server) updateCommitSyncTimestamp(ctx context.Context, projectID string, value time.Time, a actor) error {
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO project_commit_sync_state(project_id,last_synced_at,updated_by_type,updated_by_id) VALUES(?,?,?,?) ON CONFLICT(project_id) DO UPDATE SET last_synced_at=excluded.last_synced_at,updated_by_type=excluded.updated_by_type,updated_by_id=excluded.updated_by_id`, projectID, value.Format(time.RFC3339Nano), a.Type, nullableString(a.ID))
	return err
}

func (s *Server) recordRecentCommits(ctx context.Context, projectID, repositoryID, branch string, commits []providers.RecentCommit, a actor) (int, int, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,stage FROM work_items WHERE project_id=?`, projectID)
	if err != nil {
		return 0, 0, err
	}
	type workItem struct{ id, stage string }
	items := []workItem{}
	for rows.Next() {
		var item workItem
		if err = rows.Scan(&item.id, &item.stage); err != nil {
			rows.Close()
			return 0, 0, err
		}
		items = append(items, item)
	}
	rows.Close()
	inserted, matches := 0, 0
	for _, commit := range commits {
		for _, item := range items {
			if !containsWorkItemID(commit.Message, item.id) {
				continue
			}
			matches++
			result, execErr := s.store.DB.ExecContext(ctx, `INSERT OR IGNORE INTO git_commit_evidence(id,work_item_id,repository_id,commit_sha,message,author,branch,files_json,created_at) VALUES(?,?,?,?,?,?,?,'[]',?)`, security.Token(18), item.id, repositoryID, commit.SHA, commit.Message, nullableString(commit.Author), branch, now())
			if execErr != nil {
				return inserted, matches, execErr
			}
			changed, _ := result.RowsAffected()
			if changed == 0 {
				continue
			}
			inserted++
			payload, _ := json.Marshal(map[string]any{"sha": commit.SHA, "message": commit.Message, "author": commit.Author, "branch": branch, "webUrl": commit.WebURL})
			_, _ = s.store.DB.ExecContext(ctx, `INSERT INTO conversation_entries(id,work_item_id,kind,stage,author_type,author_id,payload_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, security.Token(18), item.id, "commit_evidence", item.stage, a.Type, nullableString(a.ID), string(payload), now())
		}
	}
	return inserted, matches, nil
}

func containsWorkItemID(message, workItemID string) bool {
	message = strings.ToUpper(message)
	workItemID = strings.ToUpper(workItemID)
	for start := 0; ; {
		index := strings.Index(message[start:], workItemID)
		if index < 0 {
			return false
		}
		index += start
		beforeOK := index == 0 || !isWorkItemIDCharacter(message[index-1])
		after := index + len(workItemID)
		afterOK := after == len(message) || !isWorkItemIDCharacter(message[after])
		if beforeOK && afterOK {
			return true
		}
		start = index + 1
	}
}

func isWorkItemIDCharacter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '-'
}
