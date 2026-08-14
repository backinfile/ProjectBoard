package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/projectboard/projectboard/internal/security"
	"github.com/projectboard/projectboard/internal/store"
)

type Actor struct{ Type, ID string }

type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string { return e.Message }
func IsCode(err error, code string) bool {
	var target *Error
	return errors.As(err, &target) && target.Code == code
}

type Node struct {
	ID                 string  `json:"id"`
	ProjectID          string  `json:"projectId"`
	ParentID           *string `json:"parentId"`
	Title              string  `json:"title"`
	Markdown           string  `json:"markdown"`
	Summary            string  `json:"summary"`
	TriggerDescription string  `json:"triggerDescription"`
	SortOrder          int     `json:"sortOrder"`
	LockedForAgents    bool    `json:"lockedForAgents"`
	Version            int64   `json:"version"`
	CreatedByType      string  `json:"createdByType"`
	CreatedByID        *string `json:"createdById,omitempty"`
	CreatedAt          string  `json:"createdAt"`
	UpdatedByType      string  `json:"updatedByType"`
	UpdatedByID        *string `json:"updatedById,omitempty"`
	UpdatedAt          string  `json:"updatedAt"`
}

type Revision struct {
	Version            int64   `json:"version"`
	ParentID           *string `json:"parentId"`
	Title              string  `json:"title"`
	Markdown           string  `json:"markdown"`
	Summary            string  `json:"summary"`
	TriggerDescription string  `json:"triggerDescription"`
	SortOrder          int     `json:"sortOrder"`
	LockedForAgents    bool    `json:"lockedForAgents"`
	ActorType          string  `json:"actorType"`
	ActorID            *string `json:"actorId,omitempty"`
	Reason             string  `json:"reason"`
	CreatedAt          string  `json:"createdAt"`
}

type CreateInput struct {
	ProjectID, ParentID, Title, Markdown string
	Summary, TriggerDescription          string
	SortOrder                            int
}

type UpdateInput struct {
	ID                          string
	ExpectedVersion             int64
	Title, Markdown             string
	Summary, TriggerDescription string
}

type Operation struct {
	Type               string `json:"type"`
	NodeID             string `json:"nodeId,omitempty"`
	ParentID           string `json:"parentId,omitempty"`
	Title              string `json:"title,omitempty"`
	Markdown           string `json:"markdown,omitempty"`
	Summary            string `json:"summary,omitempty"`
	TriggerDescription string `json:"triggerDescription,omitempty"`
	SortOrder          int    `json:"sortOrder,omitempty"`
	ExpectedVersion    int64  `json:"expectedVersion,omitempty"`
}

type Module struct {
	store *store.Store
	now   func() time.Time
}

func New(s *store.Store) *Module { return &Module{store: s, now: time.Now} }

func (m *Module) Create(ctx context.Context, actor Actor, in CreateInput) (*Node, error) {
	if strings.TrimSpace(in.ProjectID) == "" || strings.TrimSpace(in.Title) == "" {
		return nil, &Error{422, "VALIDATION_ERROR", "Project and title are required"}
	}
	id := security.Token(18)
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		return m.createTx(ctx, tx, actor, id, in)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, id)
}

func (m *Module) Update(ctx context.Context, actor Actor, in UpdateInput) (*Node, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, &Error{422, "VALIDATION_ERROR", "Title is required"}
	}
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		return m.updateTx(ctx, tx, actor, in)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, in.ID)
}

func (m *Module) Move(ctx context.Context, actor Actor, id string, expectedVersion int64, parentID string, sortOrder int) (*Node, error) {
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		return m.moveTx(ctx, tx, actor, id, expectedVersion, parentID, sortOrder)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, id)
}

func (m *Module) Delete(ctx context.Context, actor Actor, id string, expectedVersion int64) error {
	return m.store.Write(ctx, func(tx *sql.Tx) error {
		return m.deleteTx(ctx, tx, actor, id, expectedVersion)
	})
}

func (m *Module) Apply(ctx context.Context, actor Actor, projectID string, operations []Operation) ([]Node, error) {
	if len(operations) > 100 {
		return nil, &Error{422, "INVALID_OPERATIONS", "Provide no more than 100 knowledge operations"}
	}
	touched := []string{}
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		for _, operation := range operations {
			if operation.NodeID != "" {
				var owner string
				if err := tx.QueryRowContext(ctx, "SELECT project_id FROM knowledge_nodes WHERE id=?", operation.NodeID).Scan(&owner); err != nil || owner != projectID {
					return &Error{422, "INVALID_NODE", "Knowledge operation node must belong to the request project"}
				}
			}
			switch operation.Type {
			case "create":
				newID := security.Token(18)
				if err := m.createTx(ctx, tx, actor, newID, CreateInput{ProjectID: projectID, ParentID: operation.ParentID, Title: operation.Title, Markdown: operation.Markdown, Summary: operation.Summary, TriggerDescription: operation.TriggerDescription, SortOrder: operation.SortOrder}); err != nil {
					return err
				}
				touched = append(touched, newID)
			case "update":
				if err := m.updateTx(ctx, tx, actor, UpdateInput{ID: operation.NodeID, ExpectedVersion: operation.ExpectedVersion, Title: operation.Title, Markdown: operation.Markdown, Summary: operation.Summary, TriggerDescription: operation.TriggerDescription}); err != nil {
					return err
				}
				touched = append(touched, operation.NodeID)
			case "move":
				if err := m.moveTx(ctx, tx, actor, operation.NodeID, operation.ExpectedVersion, operation.ParentID, operation.SortOrder); err != nil {
					return err
				}
				touched = append(touched, operation.NodeID)
			case "delete":
				if err := m.deleteTx(ctx, tx, actor, operation.NodeID, operation.ExpectedVersion); err != nil {
					return err
				}
			default:
				return &Error{422, "INVALID_OPERATION", "Unknown knowledge operation: " + operation.Type}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := []Node{}
	for _, id := range touched {
		if node, getErr := m.Get(ctx, id); getErr == nil {
			out = append(out, *node)
		}
	}
	return out, nil
}

func (m *Module) SetLock(ctx context.Context, actor Actor, id string, expectedVersion int64, locked bool) (*Node, error) {
	if actor.Type != "human" {
		return nil, &Error{403, "HUMAN_REQUIRED", "Only members can change Agent locks"}
	}
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		current, err := getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return &Error{409, "VERSION_CONFLICT", "Knowledge node version changed"}
		}
		stamp := m.timestamp()
		_, err = tx.ExecContext(ctx, `UPDATE knowledge_nodes SET locked_for_agents=?,version=version+1,updated_by_type=?,updated_by_id=?,updated_at=? WHERE id=?`, boolInt(locked), actor.Type, nullable(actor.ID), stamp, id)
		if err != nil {
			return err
		}
		return saveRevision(ctx, tx, id, actor, "lock changed", stamp)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, id)
}

func (m *Module) Restore(ctx context.Context, actor Actor, id string, expectedVersion, revisionVersion int64) (*Node, error) {
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		current, err := getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err = canMutate(actor, current, expectedVersion); err != nil {
			return err
		}
		var revision Revision
		var locked int
		err = tx.QueryRowContext(ctx, `SELECT parent_id,title,markdown,summary,trigger_description,sort_order,locked_for_agents FROM knowledge_node_revisions WHERE node_id=? AND version=?`, id, revisionVersion).Scan(&revision.ParentID, &revision.Title, &revision.Markdown, &revision.Summary, &revision.TriggerDescription, &revision.SortOrder, &locked)
		if err != nil {
			return err
		}
		revision.LockedForAgents = locked != 0
		stamp := m.timestamp()
		_, err = tx.ExecContext(ctx, `UPDATE knowledge_nodes SET parent_id=?,title=?,markdown=?,summary=?,trigger_description=?,sort_order=?,locked_for_agents=?,version=version+1,updated_by_type=?,updated_by_id=?,updated_at=? WHERE id=?`, revision.ParentID, revision.Title, revision.Markdown, revision.Summary, revision.TriggerDescription, revision.SortOrder, boolInt(revision.LockedForAgents), actor.Type, nullable(actor.ID), stamp, id)
		if err != nil {
			return err
		}
		if err = upsertSearchTx(ctx, tx, id); err != nil {
			return err
		}
		return saveRevision(ctx, tx, id, actor, "restored revision", stamp)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, id)
}

func (m *Module) Get(ctx context.Context, id string) (*Node, error) {
	node, err := scanNode(m.store.DB.QueryRowContext(ctx, nodeSelect+" WHERE id=? AND deleted_at IS NULL", id))
	if err == sql.ErrNoRows {
		return nil, &Error{404, "NOT_FOUND", "Knowledge node not found"}
	}
	if err != nil {
		return nil, err
	}
	return node, nil
}

func (m *Module) List(ctx context.Context, projectID, query string) ([]Node, error) {
	statement := nodeSelect + " WHERE project_id=? AND deleted_at IS NULL"
	args := []any{projectID}
	if strings.TrimSpace(query) != "" {
		statement += " AND (title LIKE ? OR markdown LIKE ? OR summary LIKE ? OR trigger_description LIKE ?)"
		like := "%" + strings.TrimSpace(query) + "%"
		args = append(args, like, like, like, like)
	}
	statement += " ORDER BY parent_id,sort_order,title,id"
	rows, err := m.store.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Node{}
	for rows.Next() {
		node, scanErr := scanNode(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *node)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type SearchHit struct {
	NodeID  string  `json:"nodeId"`
	Title   string  `json:"title"`
	Summary string  `json:"summary"`
	Snippet string  `json:"snippet"`
	Version int64   `json:"version"`
	Score   float64 `json:"score"`
}

func (m *Module) Search(ctx context.Context, projectID, query string, limit int) ([]SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []SearchHit{}, nil
	}
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	terms := strings.Fields(query)
	for index, term := range terms {
		terms[index] = `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
	}
	match := strings.Join(terms, " OR ")
	rows, err := m.store.DB.QueryContext(ctx, `SELECT n.id,n.title,n.summary,snippet(knowledge_search,5,'','', ' … ',24),n.version,bm25(knowledge_search,0.0,0.0,8.0,5.0,4.0,1.0)
		FROM knowledge_search JOIN knowledge_nodes n ON n.id=knowledge_search.node_id
		WHERE knowledge_search MATCH ? AND knowledge_search.project_id=? AND n.deleted_at IS NULL
		ORDER BY bm25(knowledge_search,0.0,0.0,8.0,5.0,4.0,1.0),n.title,n.id LIMIT ?`, match, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var hit SearchHit
		if err = rows.Scan(&hit.NodeID, &hit.Title, &hit.Summary, &hit.Snippet, &hit.Version, &hit.Score); err != nil {
			return nil, err
		}
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

func (m *Module) Catalog(ctx context.Context, projectID string) (string, error) {
	nodes, err := m.List(ctx, projectID, "")
	if err != nil {
		return "", err
	}
	paths := nodePaths(nodes)
	sort.Slice(nodes, func(i, j int) bool { return paths[nodes[i].ID] < paths[nodes[j].ID] })
	var catalog strings.Builder
	catalog.WriteString("# Project knowledge catalog\n\n")
	for index, node := range nodes {
		var entry strings.Builder
		fmt.Fprintf(&entry, "- %s (node_id: %s, version: %d)\n", paths[node.ID], node.ID, node.Version)
		if strings.TrimSpace(node.Summary) != "" {
			fmt.Fprintf(&entry, "  Summary: %s\n", strings.TrimSpace(node.Summary))
		}
		if strings.TrimSpace(node.TriggerDescription) != "" {
			fmt.Fprintf(&entry, "  Use when: %s\n", strings.TrimSpace(node.TriggerDescription))
		}
		if catalog.Len()+entry.Len() > 7900 {
			fmt.Fprintf(&catalog, "\n%d additional nodes omitted from the catalog budget; use knowledge_search to discover them.\n", len(nodes)-index)
			break
		}
		catalog.WriteString(entry.String())
	}
	return catalog.String(), nil
}

func (m *Module) GetForProject(ctx context.Context, projectID, id string) (*Node, error) {
	node, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if node.ProjectID != projectID {
		return nil, &Error{404, "NOT_FOUND", "Knowledge node not found"}
	}
	return node, nil
}

func (m *Module) Snapshot(ctx context.Context, projectID string) (string, error) {
	nodes, err := m.List(ctx, projectID, "")
	if err != nil {
		return "", err
	}
	paths := nodePaths(nodes)
	sort.Slice(nodes, func(i, j int) bool {
		return paths[nodes[i].ID] < paths[nodes[j].ID]
	})
	var snapshot strings.Builder
	snapshot.WriteString("# Project knowledge snapshot\n\n")
	for _, node := range nodes {
		fmt.Fprintf(&snapshot, "## %s\nnode_id: %s\nversion: %d\nlocked_for_agents: %t\nsummary: %s\ntrigger_description: %s\n", paths[node.ID], node.ID, node.Version, node.LockedForAgents, node.Summary, node.TriggerDescription)
		snapshot.WriteString("\n" + node.Markdown + "\n\n")
	}
	return snapshot.String(), nil
}

func (m *Module) Revisions(ctx context.Context, id string) ([]Revision, error) {
	rows, err := m.store.DB.QueryContext(ctx, `SELECT version,parent_id,title,markdown,summary,trigger_description,sort_order,locked_for_agents,actor_type,actor_id,reason,created_at FROM knowledge_node_revisions WHERE node_id=? ORDER BY version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Revision{}
	for rows.Next() {
		var revision Revision
		var locked int
		if err = rows.Scan(&revision.Version, &revision.ParentID, &revision.Title, &revision.Markdown, &revision.Summary, &revision.TriggerDescription, &revision.SortOrder, &locked, &revision.ActorType, &revision.ActorID, &revision.Reason, &revision.CreatedAt); err != nil {
			return nil, err
		}
		revision.LockedForAgents = locked != 0
		out = append(out, revision)
	}
	return out, rows.Err()
}

func (m *Module) createTx(ctx context.Context, tx *sql.Tx, actor Actor, id string, in CreateInput) error {
	if strings.TrimSpace(in.ProjectID) == "" || strings.TrimSpace(in.Title) == "" {
		return &Error{422, "VALIDATION_ERROR", "Project and title are required"}
	}
	if err := validateDiscoveryMetadata(in.Summary, in.TriggerDescription); err != nil {
		return err
	}
	if in.ParentID != "" {
		var projectID string
		if err := tx.QueryRowContext(ctx, "SELECT project_id FROM knowledge_nodes WHERE id=? AND deleted_at IS NULL", in.ParentID).Scan(&projectID); err != nil || projectID != in.ProjectID {
			return &Error{422, "INVALID_PARENT", "Parent must be an active node in the same project"}
		}
	}
	stamp := m.timestamp()
	_, err := tx.ExecContext(ctx, `INSERT INTO knowledge_nodes(id,project_id,parent_id,title,markdown,summary,trigger_description,sort_order,created_by_type,created_by_id,created_at,updated_by_type,updated_by_id,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, in.ProjectID, nullable(in.ParentID), strings.TrimSpace(in.Title), in.Markdown, strings.TrimSpace(in.Summary), strings.TrimSpace(in.TriggerDescription), in.SortOrder, actor.Type, nullable(actor.ID), stamp, actor.Type, nullable(actor.ID), stamp)
	if err != nil {
		return err
	}
	if err = upsertSearchTx(ctx, tx, id); err != nil {
		return err
	}
	return saveRevision(ctx, tx, id, actor, "created", stamp)
}

func (m *Module) updateTx(ctx context.Context, tx *sql.Tx, actor Actor, in UpdateInput) error {
	if strings.TrimSpace(in.Title) == "" {
		return &Error{422, "VALIDATION_ERROR", "Title is required"}
	}
	if err := validateDiscoveryMetadata(in.Summary, in.TriggerDescription); err != nil {
		return err
	}
	current, err := getTx(ctx, tx, in.ID)
	if err != nil {
		return err
	}
	if err = canMutate(actor, current, in.ExpectedVersion); err != nil {
		return err
	}
	stamp := m.timestamp()
	_, err = tx.ExecContext(ctx, `UPDATE knowledge_nodes SET title=?,markdown=?,summary=?,trigger_description=?,version=version+1,updated_by_type=?,updated_by_id=?,updated_at=? WHERE id=?`, strings.TrimSpace(in.Title), in.Markdown, strings.TrimSpace(in.Summary), strings.TrimSpace(in.TriggerDescription), actor.Type, nullable(actor.ID), stamp, in.ID)
	if err != nil {
		return err
	}
	if err = upsertSearchTx(ctx, tx, in.ID); err != nil {
		return err
	}
	return saveRevision(ctx, tx, in.ID, actor, "updated", stamp)
}

func (m *Module) moveTx(ctx context.Context, tx *sql.Tx, actor Actor, id string, expectedVersion int64, parentID string, sortOrder int) error {
	current, err := getTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if err = canMutate(actor, current, expectedVersion); err != nil {
		return err
	}
	if parentID == id {
		return &Error{422, "INVALID_PARENT", "A node cannot be its own parent"}
	}
	if parentID != "" {
		var owner string
		if err = tx.QueryRowContext(ctx, "SELECT project_id FROM knowledge_nodes WHERE id=? AND deleted_at IS NULL", parentID).Scan(&owner); err != nil || owner != current.ProjectID {
			return &Error{422, "INVALID_PARENT", "Parent must be an active node in the same project"}
		}
		var cycle int
		if err = tx.QueryRowContext(ctx, `WITH RECURSIVE descendants(id) AS (SELECT id FROM knowledge_nodes WHERE parent_id=? AND deleted_at IS NULL UNION ALL SELECT n.id FROM knowledge_nodes n JOIN descendants d ON n.parent_id=d.id WHERE n.deleted_at IS NULL) SELECT COUNT(*) FROM descendants WHERE id=?`, id, parentID).Scan(&cycle); err != nil {
			return err
		}
		if cycle > 0 {
			return &Error{422, "INVALID_PARENT", "A node cannot move below its descendant"}
		}
	}
	stamp := m.timestamp()
	_, err = tx.ExecContext(ctx, `UPDATE knowledge_nodes SET parent_id=?,sort_order=?,version=version+1,updated_by_type=?,updated_by_id=?,updated_at=? WHERE id=?`, nullable(parentID), sortOrder, actor.Type, nullable(actor.ID), stamp, id)
	if err != nil {
		return err
	}
	return saveRevision(ctx, tx, id, actor, "moved", stamp)
}

func (m *Module) deleteTx(ctx context.Context, tx *sql.Tx, actor Actor, id string, expectedVersion int64) error {
	current, err := getTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if err = canMutate(actor, current, expectedVersion); err != nil {
		return err
	}
	if actor.Type == "agent" {
		var locked int
		err = tx.QueryRowContext(ctx, `WITH RECURSIVE subtree(id,locked) AS (SELECT id,locked_for_agents FROM knowledge_nodes WHERE id=? AND deleted_at IS NULL UNION ALL SELECT n.id,n.locked_for_agents FROM knowledge_nodes n JOIN subtree s ON n.parent_id=s.id WHERE n.deleted_at IS NULL) SELECT COALESCE(SUM(locked),0) FROM subtree`, id).Scan(&locked)
		if err != nil {
			return err
		}
		if locked > 0 {
			return &Error{409, "NODE_LOCKED", "A locked knowledge node exists in the deleted subtree"}
		}
	}
	stamp := m.timestamp()
	_, err = tx.ExecContext(ctx, `WITH RECURSIVE subtree(id) AS (SELECT id FROM knowledge_nodes WHERE id=? AND deleted_at IS NULL UNION ALL SELECT n.id FROM knowledge_nodes n JOIN subtree s ON n.parent_id=s.id WHERE n.deleted_at IS NULL) DELETE FROM knowledge_search WHERE node_id IN (SELECT id FROM subtree)`, id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `WITH RECURSIVE subtree(id) AS (SELECT id FROM knowledge_nodes WHERE id=? AND deleted_at IS NULL UNION ALL SELECT n.id FROM knowledge_nodes n JOIN subtree s ON n.parent_id=s.id WHERE n.deleted_at IS NULL) UPDATE knowledge_nodes SET deleted_at=?,version=version+1,updated_by_type=?,updated_by_id=?,updated_at=? WHERE id IN (SELECT id FROM subtree)`, id, stamp, actor.Type, nullable(actor.ID), stamp)
	return err
}

func (m *Module) timestamp() string { return m.now().UTC().Format(time.RFC3339Nano) }

const nodeSelect = `SELECT id,project_id,parent_id,title,markdown,summary,trigger_description,sort_order,locked_for_agents,version,created_by_type,created_by_id,created_at,updated_by_type,updated_by_id,updated_at FROM knowledge_nodes`

type scanner interface{ Scan(...any) error }

func scanNode(row scanner) (*Node, error) {
	var node Node
	var locked int
	err := row.Scan(&node.ID, &node.ProjectID, &node.ParentID, &node.Title, &node.Markdown, &node.Summary, &node.TriggerDescription, &node.SortOrder, &locked, &node.Version, &node.CreatedByType, &node.CreatedByID, &node.CreatedAt, &node.UpdatedByType, &node.UpdatedByID, &node.UpdatedAt)
	node.LockedForAgents = locked != 0
	return &node, err
}

func getTx(ctx context.Context, tx *sql.Tx, id string) (*Node, error) {
	node, err := scanNode(tx.QueryRowContext(ctx, nodeSelect+" WHERE id=? AND deleted_at IS NULL", id))
	if err == sql.ErrNoRows {
		return nil, &Error{404, "NOT_FOUND", "Knowledge node not found"}
	}
	return node, err
}

func canMutate(actor Actor, node *Node, expectedVersion int64) error {
	if node.Version != expectedVersion {
		return &Error{409, "VERSION_CONFLICT", "Knowledge node version changed"}
	}
	if actor.Type == "agent" && node.LockedForAgents {
		return &Error{409, "NODE_LOCKED", "Knowledge node is locked for Agents"}
	}
	return nil
}

func saveRevision(ctx context.Context, tx *sql.Tx, id string, actor Actor, reason, stamp string) error {
	node, err := getTx(ctx, tx, id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO knowledge_node_revisions(id,node_id,version,parent_id,title,markdown,summary,trigger_description,sort_order,locked_for_agents,file_ids_json,actor_type,actor_id,reason,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, security.Token(18), id, node.Version, node.ParentID, node.Title, node.Markdown, node.Summary, node.TriggerDescription, node.SortOrder, boolInt(node.LockedForAgents), "[]", actor.Type, nullable(actor.ID), reason, stamp)
	return err
}

func upsertSearchTx(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_search WHERE node_id=?`, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO knowledge_search(node_id,project_id,title,summary,trigger_description,markdown)
		SELECT id,project_id,title,summary,trigger_description,markdown FROM knowledge_nodes WHERE id=? AND deleted_at IS NULL`, id)
	return err
}

func nodePaths(nodes []Node) map[string]string {
	byID := map[string]Node{}
	for _, node := range nodes {
		byID[node.ID] = node
	}
	paths := map[string]string{}
	var pathFor func(Node, map[string]bool) string
	pathFor = func(node Node, seen map[string]bool) string {
		if path, ok := paths[node.ID]; ok {
			return path
		}
		if seen[node.ID] || node.ParentID == nil {
			return node.Title
		}
		seen[node.ID] = true
		parent, ok := byID[*node.ParentID]
		if !ok {
			return node.Title
		}
		return pathFor(parent, seen) + "/" + node.Title
	}
	for _, node := range nodes {
		paths[node.ID] = pathFor(node, map[string]bool{})
	}
	return paths
}

func validateDiscoveryMetadata(summary, trigger string) error {
	if utf8.RuneCountInString(summary) > 1000 || utf8.RuneCountInString(trigger) > 1000 {
		return &Error{422, "VALIDATION_ERROR", "Knowledge summary and trigger description must each be 1000 characters or fewer"}
	}
	return nil
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
