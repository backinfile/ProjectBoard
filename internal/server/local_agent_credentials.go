package server

import (
	"context"
	"fmt"

	"github.com/projectboard/projectboard/internal/agentexec"
)

type localCredentialProvider struct{ server *Server }

func (p *localCredentialProvider) Issue(ctx context.Context, projectID string) (*agentexec.Credential, error) {
	var installationID, repositoryID string
	if err := p.server.store.DB.QueryRowContext(ctx, `SELECT a.installation_id,g.repository_id FROM project_repository_grants g JOIN provider_authorizations a ON a.id=g.authorization_id WHERE g.project_id=? AND g.revoked_at IS NULL AND g.access_level='write' AND a.status='active' AND a.provider='github'`, projectID).Scan(&installationID, &repositoryID); err != nil {
		return nil, fmt.Errorf("a writable GitHub repository grant is required: %w", err)
	}
	issued, err := p.server.gitClient().IssueExecutionCredential(ctx, installationID, repositoryID)
	if err != nil {
		return nil, err
	}
	return &agentexec.Credential{Token: issued.Token, Revoke: func(revokeCtx context.Context) error {
		return p.server.gitClient().RevokeExecutionCredential(revokeCtx, issued.Token)
	}}, nil
}
