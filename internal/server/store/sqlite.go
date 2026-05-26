package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"lark/internal/proto"
	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// --- Workspaces ---

func (s *SQLiteStore) CreateWorkspace(ctx context.Context, ws *proto.Workspace) error {
	if ws.ID == "" {
		ws.ID = uuid.New().String()
	}
	now := time.Now().UnixMilli()
	ws.CreatedAt = now
	ws.UpdatedAt = now
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workspaces (id, name, slug, icon_url, agent_provision_token, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ws.ID, ws.Name, ws.Slug, ws.IconURL, ws.AgentProvisionToken, ws.CreatedAt, ws.UpdatedAt)
	return err
}

func (s *SQLiteStore) GetWorkspace(ctx context.Context, id string) (*proto.Workspace, error) {
	ws := &proto.Workspace{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, icon_url, agent_provision_token, created_at, updated_at
		 FROM workspaces WHERE id = ?`, id).
		Scan(&ws.ID, &ws.Name, &ws.Slug, &ws.IconURL, &ws.AgentProvisionToken, &ws.CreatedAt, &ws.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return ws, err
}

func (s *SQLiteStore) GetWorkspaceBySlug(ctx context.Context, slug string) (*proto.Workspace, error) {
	ws := &proto.Workspace{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, icon_url, agent_provision_token, created_at, updated_at
		 FROM workspaces WHERE slug = ?`, slug).
		Scan(&ws.ID, &ws.Name, &ws.Slug, &ws.IconURL, &ws.AgentProvisionToken, &ws.CreatedAt, &ws.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return ws, err
}

func (s *SQLiteStore) ListWorkspaces(ctx context.Context) ([]*proto.Workspace, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, slug, icon_url, agent_provision_token, created_at, updated_at FROM workspaces ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*proto.Workspace
	for rows.Next() {
		ws := &proto.Workspace{}
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.Slug, &ws.IconURL, &ws.AgentProvisionToken, &ws.CreatedAt, &ws.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateWorkspace(ctx context.Context, ws *proto.Workspace) error {
	ws.UpdatedAt = time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx,
		`UPDATE workspaces SET name=?, slug=?, icon_url=?, updated_at=? WHERE id=?`,
		ws.Name, ws.Slug, ws.IconURL, ws.UpdatedAt, ws.ID)
	return err
}

// --- Members ---

func (s *SQLiteStore) CreateMember(ctx context.Context, m *proto.Member) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	now := time.Now().UnixMilli()
	m.CreatedAt = now
	m.UpdatedAt = now
	roleCard, _ := json.Marshal(m.RoleCard)
	caps, _ := json.Marshal(m.Capabilities)
	runtime, _ := json.Marshal(m.RuntimeInfo)
	var apiKey interface{}
	if m.APIKey != "" {
		apiKey = m.APIKey
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO members (id, workspace_id, name, type, avatar_url, status, api_key, role_card, capabilities, runtime_info, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.WorkspaceID, m.Name, string(m.Type), m.AvatarURL, string(m.Status),
		apiKey, roleCard, caps, runtime, m.CreatedAt, m.UpdatedAt)
	return err
}

func (s *SQLiteStore) GetMember(ctx context.Context, id string) (*proto.Member, error) {
	m := &proto.Member{}
	var memberType, status string
	var apiKey sql.NullString
	var roleCard, caps, runtime []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, name, type, avatar_url, status, api_key, role_card, capabilities, runtime_info, created_at, updated_at
		 FROM members WHERE id = ?`, id).
		Scan(&m.ID, &m.WorkspaceID, &m.Name, &memberType, &m.AvatarURL, &status,
			&apiKey, &roleCard, &caps, &runtime, &m.CreatedAt, &m.UpdatedAt)
	m.APIKey = apiKey.String
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.Type = proto.MemberType(memberType)
	m.Status = proto.Presence(status)
	if len(roleCard) > 0 {
		json.Unmarshal(roleCard, &m.RoleCard)
	}
	if len(caps) > 0 {
		json.Unmarshal(caps, &m.Capabilities)
	}
	if len(runtime) > 0 {
		json.Unmarshal(runtime, &m.RuntimeInfo)
	}
	return m, nil
}

func (s *SQLiteStore) GetMemberByAPIKey(ctx context.Context, key string) (*proto.Member, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM members WHERE api_key = ?`, key).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetMember(ctx, id)
}

func (s *SQLiteStore) GetMemberByName(ctx context.Context, workspaceID, name string) (*proto.Member, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM members WHERE workspace_id = ? AND name = ?`, workspaceID, name).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetMember(ctx, id)
}

func (s *SQLiteStore) UpdateMemberRoleCard(ctx context.Context, memberID string, roleCard *proto.RoleCard, runtime *proto.RuntimeInfo) error {
	rc, _ := json.Marshal(roleCard)
	rt, _ := json.Marshal(runtime)
	_, err := s.db.ExecContext(ctx,
		`UPDATE members SET role_card=?, runtime_info=?, updated_at=? WHERE id=?`,
		rc, rt, time.Now().UnixMilli(), memberID)
	return err
}

func (s *SQLiteStore) ListMembers(ctx context.Context, workspaceID string) ([]*proto.Member, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, workspace_id, name, type, avatar_url, status, api_key, role_card, capabilities, runtime_info, created_at, updated_at
		 FROM members WHERE workspace_id = ? ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMembers(rows)
}

func scanMembers(rows *sql.Rows) ([]*proto.Member, error) {
	var out []*proto.Member
	for rows.Next() {
		m := &proto.Member{}
		var memberType, status string
		var apiKey sql.NullString
		var roleCard, caps, runtime []byte
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.Name, &memberType, &m.AvatarURL, &status,
			&apiKey, &roleCard, &caps, &runtime, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.APIKey = apiKey.String
		m.Type = proto.MemberType(memberType)
		m.Status = proto.Presence(status)
		if len(roleCard) > 0 {
			json.Unmarshal(roleCard, &m.RoleCard)
		}
		if len(caps) > 0 {
			json.Unmarshal(caps, &m.Capabilities)
		}
		if len(runtime) > 0 {
			json.Unmarshal(runtime, &m.RuntimeInfo)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateMember(ctx context.Context, m *proto.Member) error {
	m.UpdatedAt = time.Now().UnixMilli()
	roleCard, _ := json.Marshal(m.RoleCard)
	caps, _ := json.Marshal(m.Capabilities)
	runtime, _ := json.Marshal(m.RuntimeInfo)
	_, err := s.db.ExecContext(ctx,
		`UPDATE members SET name=?, avatar_url=?, status=?, role_card=?, capabilities=?, runtime_info=?, updated_at=?
		 WHERE id=?`,
		m.Name, m.AvatarURL, string(m.Status), roleCard, caps, runtime, m.UpdatedAt, m.ID)
	return err
}

func (s *SQLiteStore) DeleteMember(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM members WHERE id = ?`, id)
	return err
}

// --- Channels ---

func (s *SQLiteStore) CreateChannel(ctx context.Context, ch *proto.Channel) error {
	if ch.ID == "" {
		ch.ID = uuid.New().String()
	}
	now := time.Now().UnixMilli()
	ch.CreatedAt = now
	ch.UpdatedAt = now
	private := 0
	if ch.IsPrivate {
		private = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO channels (id, workspace_id, name, type, topic, is_private, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ch.ID, ch.WorkspaceID, ch.Name, string(ch.Type), ch.Topic, private, ch.CreatedAt, ch.UpdatedAt)
	return err
}

func (s *SQLiteStore) GetChannel(ctx context.Context, id string) (*proto.Channel, error) {
	ch := &proto.Channel{}
	var chType string
	var private int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, name, type, topic, is_private, created_at, updated_at
		 FROM channels WHERE id = ?`, id).
		Scan(&ch.ID, &ch.WorkspaceID, &ch.Name, &chType, &ch.Topic, &private, &ch.CreatedAt, &ch.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ch.Type = proto.ChannelType(chType)
	ch.IsPrivate = private == 1
	return ch, nil
}

func (s *SQLiteStore) ListChannels(ctx context.Context, workspaceID string) ([]*proto.Channel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, workspace_id, name, type, topic, is_private, created_at, updated_at
		 FROM channels WHERE workspace_id = ? ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*proto.Channel
	for rows.Next() {
		ch := &proto.Channel{}
		var chType string
		var private int
		if err := rows.Scan(&ch.ID, &ch.WorkspaceID, &ch.Name, &chType, &ch.Topic, &private, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, err
		}
		ch.Type = proto.ChannelType(chType)
		ch.IsPrivate = private == 1
		out = append(out, ch)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateChannel(ctx context.Context, ch *proto.Channel) error {
	ch.UpdatedAt = time.Now().UnixMilli()
	private := 0
	if ch.IsPrivate {
		private = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE channels SET name=?, topic=?, is_private=?, updated_at=? WHERE id=?`,
		ch.Name, ch.Topic, private, ch.UpdatedAt, ch.ID)
	return err
}

func (s *SQLiteStore) DeleteChannel(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM channels WHERE id = ?`, id)
	return err
}

// --- Channel Members ---

func (s *SQLiteStore) AddChannelMember(ctx context.Context, channelID, memberID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO channel_members (channel_id, member_id) VALUES (?, ?)`,
		channelID, memberID)
	return err
}

func (s *SQLiteStore) RemoveChannelMember(ctx context.Context, channelID, memberID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM channel_members WHERE channel_id = ? AND member_id = ?`,
		channelID, memberID)
	return err
}

func (s *SQLiteStore) ListChannelMembers(ctx context.Context, channelID string) ([]*proto.Member, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.id, m.workspace_id, m.name, m.type, m.avatar_url, m.status, m.api_key, m.role_card, m.capabilities, m.runtime_info, m.created_at, m.updated_at
		 FROM members m JOIN channel_members cm ON m.id = cm.member_id
		 WHERE cm.channel_id = ? ORDER BY m.name`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMembers(rows)
}

func (s *SQLiteStore) IsChannelMember(ctx context.Context, channelID, memberID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM channel_members WHERE channel_id = ? AND member_id = ?`,
		channelID, memberID).Scan(&count)
	return count > 0, err
}

func (s *SQLiteStore) UpdateLastRead(ctx context.Context, channelID, memberID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE channel_members SET last_read_at = ? WHERE channel_id = ? AND member_id = ?`,
		time.Now().UnixMilli(), channelID, memberID)
	return err
}

// --- Messages ---

func (s *SQLiteStore) CreateMessage(ctx context.Context, m *proto.Message) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	now := time.Now().UnixMilli()
	m.CreatedAt = now
	m.UpdatedAt = now
	if m.Type == "" {
		m.Type = "text"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (id, channel_id, sender_id, thread_id, content, type, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.ChannelID, m.SenderID, nullStr(m.ThreadID), m.Content, m.Type, m.Metadata, m.CreatedAt, m.UpdatedAt)
	return err
}

func (s *SQLiteStore) GetMessage(ctx context.Context, id string) (*proto.Message, error) {
	m := &proto.Message{}
	var threadID sql.NullString
	var metadata sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, channel_id, sender_id, thread_id, content, type, metadata, created_at, updated_at
		 FROM messages WHERE id = ?`, id).
		Scan(&m.ID, &m.ChannelID, &m.SenderID, &threadID, &m.Content, &m.Type, &metadata, &m.CreatedAt, &m.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.ThreadID = threadID.String
	if metadata.Valid {
		m.Metadata = json.RawMessage(metadata.String)
	}
	return m, nil
}

func (s *SQLiteStore) ListMessages(ctx context.Context, channelID string, limit, offset int) ([]*proto.Message, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_id, sender_id, thread_id, content, type, metadata, created_at, updated_at
		 FROM messages WHERE channel_id = ? AND thread_id IS NULL
		 ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		channelID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *SQLiteStore) ListMessagesBySender(ctx context.Context, senderID string) ([]*proto.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_id, sender_id, thread_id, content, type, metadata, created_at, updated_at
		 FROM messages WHERE sender_id = ? ORDER BY created_at DESC`, senderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *SQLiteStore) ListThreadMessages(ctx context.Context, threadID string) ([]*proto.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_id, sender_id, thread_id, content, type, metadata, created_at, updated_at
		 FROM messages WHERE thread_id = ? OR id = ?
		 ORDER BY created_at ASC`,
		threadID, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func scanMessages(rows *sql.Rows) ([]*proto.Message, error) {
	var out []*proto.Message
	for rows.Next() {
		m := &proto.Message{}
		var threadID sql.NullString
		var metadata sql.NullString
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.SenderID, &threadID, &m.Content, &m.Type, &metadata, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.ThreadID = threadID.String
		if metadata.Valid {
			m.Metadata = json.RawMessage(metadata.String)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateMessage(ctx context.Context, m *proto.Message) error {
	m.UpdatedAt = time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx,
		`UPDATE messages SET content=?, metadata=?, updated_at=? WHERE id=?`,
		m.Content, m.Metadata, m.UpdatedAt, m.ID)
	return err
}

func (s *SQLiteStore) DeleteMessage(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) GetRecentMessages(ctx context.Context, channelID string, limit int) ([]proto.Message, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_id, sender_id, thread_id, content, type, metadata, created_at, updated_at
		 FROM messages WHERE channel_id = ?
		 ORDER BY created_at DESC LIMIT ?`, channelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []proto.Message
	for rows.Next() {
		msg := proto.Message{}
		var threadID sql.NullString
		var metadata sql.NullString
		if err := rows.Scan(&msg.ID, &msg.ChannelID, &msg.SenderID, &threadID, &msg.Content, &msg.Type, &metadata, &msg.CreatedAt, &msg.UpdatedAt); err != nil {
			return nil, err
		}
		msg.ThreadID = threadID.String
		if metadata.Valid {
			msg.Metadata = json.RawMessage(metadata.String)
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// --- Reactions ---

func (s *SQLiteStore) AddReaction(ctx context.Context, r *proto.Reaction) error {
	r.CreatedAt = time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO reactions (message_id, member_id, emoji, created_at) VALUES (?, ?, ?, ?)`,
		r.MessageID, r.MemberID, r.Emoji, r.CreatedAt)
	return err
}

func (s *SQLiteStore) RemoveReaction(ctx context.Context, messageID, memberID, emoji string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM reactions WHERE message_id = ? AND member_id = ? AND emoji = ?`,
		messageID, memberID, emoji)
	return err
}

func (s *SQLiteStore) ListReactions(ctx context.Context, messageID string) ([]*proto.Reaction, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT message_id, member_id, emoji, created_at FROM reactions WHERE message_id = ?`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*proto.Reaction
	for rows.Next() {
		r := &proto.Reaction{}
		if err := rows.Scan(&r.MessageID, &r.MemberID, &r.Emoji, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- Search ---

func (s *SQLiteStore) SearchMessages(ctx context.Context, query string, channelID string, limit int) ([]*proto.Message, error) {
	if limit <= 0 {
		limit = 20
	}
	var rows *sql.Rows
	var err error
	if channelID != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT m.id, m.channel_id, m.sender_id, m.thread_id, m.content, m.type, m.metadata, m.created_at, m.updated_at
			 FROM messages m JOIN messages_fts fts ON m.rowid = fts.rowid
			 WHERE messages_fts MATCH ? AND m.channel_id = ?
			 ORDER BY m.created_at DESC LIMIT ?`,
			query, channelID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT m.id, m.channel_id, m.sender_id, m.thread_id, m.content, m.type, m.metadata, m.created_at, m.updated_at
			 FROM messages m JOIN messages_fts fts ON m.rowid = fts.rowid
			 WHERE messages_fts MATCH ?
			 ORDER BY m.created_at DESC LIMIT ?`,
			query, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// --- Tasks ---

func (s *SQLiteStore) CreateTask(ctx context.Context, t *proto.Task) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	now := time.Now().UnixMilli()
	t.CreatedAt = now
	t.UpdatedAt = now
	if t.Status == "" {
		t.Status = proto.TaskTodo
	}
	if t.Priority == "" {
		t.Priority = "medium"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (id, workspace_id, channel_id, assigned_to, created_by, title, description, status, priority, due_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.WorkspaceID, nullStr(t.ChannelID), nullStr(t.AssignedTo), t.CreatedBy,
		t.Title, t.Description, string(t.Status), t.Priority, t.DueAt, t.CreatedAt, t.UpdatedAt)
	return err
}

func (s *SQLiteStore) GetTask(ctx context.Context, id string) (*proto.Task, error) {
	t := &proto.Task{}
	var channelID, assignedTo sql.NullString
	var status string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, channel_id, assigned_to, created_by, title, description, status, priority, due_at, created_at, updated_at
		 FROM tasks WHERE id = ?`, id).
		Scan(&t.ID, &t.WorkspaceID, &channelID, &assignedTo, &t.CreatedBy,
			&t.Title, &t.Description, &status, &t.Priority, &t.DueAt, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.ChannelID = channelID.String
	t.AssignedTo = assignedTo.String
	t.Status = proto.TaskStatus(status)
	return t, nil
}

func (s *SQLiteStore) ListTasks(ctx context.Context, workspaceID string, status proto.TaskStatus) ([]*proto.Task, error) {
	var rows *sql.Rows
	var err error
	if status != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, workspace_id, channel_id, assigned_to, created_by, title, description, status, priority, due_at, created_at, updated_at
			 FROM tasks WHERE workspace_id = ? AND status = ? ORDER BY created_at DESC`,
			workspaceID, string(status))
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, workspace_id, channel_id, assigned_to, created_by, title, description, status, priority, due_at, created_at, updated_at
			 FROM tasks WHERE workspace_id = ? ORDER BY created_at DESC`,
			workspaceID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*proto.Task
	for rows.Next() {
		t := &proto.Task{}
		var channelID, assignedTo sql.NullString
		var statusStr string
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &channelID, &assignedTo, &t.CreatedBy,
			&t.Title, &t.Description, &statusStr, &t.Priority, &t.DueAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.ChannelID = channelID.String
		t.AssignedTo = assignedTo.String
		t.Status = proto.TaskStatus(statusStr)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateTask(ctx context.Context, t *proto.Task) error {
	t.UpdatedAt = time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET assigned_to=?, title=?, description=?, status=?, priority=?, due_at=?, updated_at=? WHERE id=?`,
		nullStr(t.AssignedTo), t.Title, t.Description, string(t.Status), t.Priority, t.DueAt, t.UpdatedAt, t.ID)
	return err
}

func (s *SQLiteStore) DeleteTask(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	return err
}

// --- Agent Memory ---

func (s *SQLiteStore) SetMemory(ctx context.Context, m *proto.AgentMemory) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	now := time.Now().UnixMilli()
	m.CreatedAt = now
	m.UpdatedAt = now
	if m.Namespace == "" {
		m.Namespace = "default"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_memory (id, agent_id, namespace, key, value, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(agent_id, namespace, key) DO UPDATE SET value=?, updated_at=?`,
		m.ID, m.AgentID, m.Namespace, m.Key, m.Value, m.CreatedAt, m.UpdatedAt,
		m.Value, m.UpdatedAt)
	return err
}

func (s *SQLiteStore) GetMemory(ctx context.Context, agentID, namespace, key string) (*proto.AgentMemory, error) {
	m := &proto.AgentMemory{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, agent_id, namespace, key, value, created_at, updated_at
		 FROM agent_memory WHERE agent_id = ? AND namespace = ? AND key = ?`,
		agentID, namespace, key).
		Scan(&m.ID, &m.AgentID, &m.Namespace, &m.Key, &m.Value, &m.CreatedAt, &m.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (s *SQLiteStore) ListMemory(ctx context.Context, agentID, namespace string) ([]*proto.AgentMemory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, namespace, key, value, created_at, updated_at
		 FROM agent_memory WHERE agent_id = ? AND namespace = ? ORDER BY key`,
		agentID, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*proto.AgentMemory
	for rows.Next() {
		m := &proto.AgentMemory{}
		if err := rows.Scan(&m.ID, &m.AgentID, &m.Namespace, &m.Key, &m.Value, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) DeleteMemory(ctx context.Context, agentID, namespace, key string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM agent_memory WHERE agent_id = ? AND namespace = ? AND key = ?`,
		agentID, namespace, key)
	return err
}

// --- Files ---

func (s *SQLiteStore) CreateFile(ctx context.Context, f *proto.File) error {
	if f.ID == "" {
		f.ID = proto.NewID()
	}
	if f.CreatedAt == 0 {
		f.CreatedAt = time.Now().UnixMilli()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO files (id, workspace_id, uploader_id, filename, mime_type, size, path, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.WorkspaceID, f.UploaderID, f.Filename, f.MimeType, f.Size, f.Path, f.CreatedAt)
	return err
}

func (s *SQLiteStore) GetFile(ctx context.Context, id string) (*proto.File, error) {
	f := &proto.File{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, uploader_id, filename, mime_type, size, path, created_at FROM files WHERE id = ?`, id).
		Scan(&f.ID, &f.WorkspaceID, &f.UploaderID, &f.Filename, &f.MimeType, &f.Size, &f.Path, &f.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}

func (s *SQLiteStore) ListFiles(ctx context.Context, workspaceID string, limit, offset int) ([]*proto.File, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, workspace_id, uploader_id, filename, mime_type, size, path, created_at
		 FROM files WHERE workspace_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		workspaceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*proto.File
	for rows.Next() {
		f := &proto.File{}
		if err := rows.Scan(&f.ID, &f.WorkspaceID, &f.UploaderID, &f.Filename, &f.MimeType, &f.Size, &f.Path, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) DeleteFile(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM files WHERE id = ?`, id)
	return err
}

// --- Pins ---

func (s *SQLiteStore) CreatePin(ctx context.Context, p *proto.Pin) error {
	if p.ID == "" {
		p.ID = proto.NewID()
	}
	if p.CreatedAt == 0 {
		p.CreatedAt = time.Now().UnixMilli()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pins (id, message_id, channel_id, pinned_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		p.ID, p.MessageID, p.ChannelID, p.PinnedBy, p.CreatedAt)
	return err
}

func (s *SQLiteStore) DeletePin(ctx context.Context, messageID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pins WHERE message_id = ?`, messageID)
	return err
}

func (s *SQLiteStore) ListPins(ctx context.Context, channelID string) ([]*proto.Pin, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, message_id, channel_id, pinned_by, created_at FROM pins WHERE channel_id = ? ORDER BY created_at DESC`,
		channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*proto.Pin
	for rows.Next() {
		p := &proto.Pin{}
		if err := rows.Scan(&p.ID, &p.MessageID, &p.ChannelID, &p.PinnedBy, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// --- DMs ---

func (s *SQLiteStore) GetDMChannel(ctx context.Context, workspaceID string, memberIDs []string) (*proto.Channel, error) {
	if len(memberIDs) < 2 {
		return nil, nil
	}
	// Find a DM channel that has exactly these members
	// For 2-member DMs, use a simple query
	if len(memberIDs) == 2 {
		var channelID string
		err := s.db.QueryRowContext(ctx,
			`SELECT cm1.channel_id FROM channel_members cm1
			 JOIN channel_members cm2 ON cm1.channel_id = cm2.channel_id
			 JOIN channels c ON c.id = cm1.channel_id
			 WHERE c.workspace_id = ? AND c.type = 'dm'
			 AND cm1.member_id = ? AND cm2.member_id = ?`,
			workspaceID, memberIDs[0], memberIDs[1]).Scan(&channelID)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return s.GetChannel(ctx, channelID)
	}
	// For group DMs, find channels that have exactly these members
	// This is more complex - for now, return nil (group DM creation handles this)
	return nil, nil
}

func (s *SQLiteStore) ListDMChannels(ctx context.Context, memberID string) ([]*proto.Channel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.workspace_id, c.name, c.type, c.topic, c.is_private, c.created_at, c.updated_at
		 FROM channels c JOIN channel_members cm ON c.id = cm.channel_id
		 WHERE cm.member_id = ? AND c.type IN ('dm', 'group_dm')
		 ORDER BY c.updated_at DESC`,
		memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*proto.Channel
	for rows.Next() {
		ch := &proto.Channel{}
		var topic, name sql.NullString
		if err := rows.Scan(&ch.ID, &ch.WorkspaceID, &name, &ch.Type, &topic, &ch.IsPrivate, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, err
		}
		ch.Name = name.String
		ch.Topic = topic.String
		out = append(out, ch)
	}
	return out, rows.Err()
}

// --- Unread counts ---

func (s *SQLiteStore) GetUnreadCounts(ctx context.Context, memberID string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT cm.channel_id, COUNT(m.id)
		 FROM channel_members cm
		 LEFT JOIN messages m ON m.channel_id = cm.channel_id
		 AND m.created_at > COALESCE(cm.last_read_at, 0)
		 WHERE cm.member_id = ?
		 GROUP BY cm.channel_id`,
		memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var channelID string
		var count int
		if err := rows.Scan(&channelID, &count); err != nil {
			return nil, err
		}
		if count > 0 {
			out[channelID] = count
		}
	}
	return out, rows.Err()
}

func (s *SQLiteStore) MarkChannelRead(ctx context.Context, channelID, memberID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE channel_members SET last_read_at = ? WHERE channel_id = ? AND member_id = ?`,
		time.Now().UnixMilli(), channelID, memberID)
	return err
}

// --- Approvals ---

func (s *SQLiteStore) CreateApproval(ctx context.Context, a *proto.ApprovalRequest) error {
	if a.ID == "" {
		a.ID = proto.NewID()
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = time.Now().UnixMilli()
	}
	a.Status = proto.ApprovalPending
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO approval_requests (id, workspace_id, agent_id, channel_id, action, payload, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.WorkspaceID, a.AgentID, a.ChannelID, a.Action, a.Payload, string(a.Status), a.CreatedAt)
	return err
}

func (s *SQLiteStore) GetApproval(ctx context.Context, id string) (*proto.ApprovalRequest, error) {
	a := &proto.ApprovalRequest{}
	var status, channelID, payload, reviewerID, reviewNote sql.NullString
	var reviewedAt sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, agent_id, channel_id, action, payload, status, reviewer_id, review_note, created_at, reviewed_at
		 FROM approval_requests WHERE id = ?`, id).
		Scan(&a.ID, &a.WorkspaceID, &a.AgentID, &channelID, &a.Action, &payload,
			&status, &reviewerID, &reviewNote, &a.CreatedAt, &reviewedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.ChannelID = channelID.String
	a.Payload = payload.String
	a.Status = proto.ApprovalStatus(status.String)
	a.ReviewerID = reviewerID.String
	a.ReviewNote = reviewNote.String
	a.ReviewedAt = reviewedAt.Int64
	return a, nil
}

func (s *SQLiteStore) ListApprovals(ctx context.Context, workspaceID string, status proto.ApprovalStatus) ([]*proto.ApprovalRequest, error) {
	query := `SELECT id, workspace_id, agent_id, channel_id, action, payload, status, reviewer_id, review_note, created_at, reviewed_at
		 FROM approval_requests WHERE workspace_id = ?`
	args := []interface{}{workspaceID}
	if status != "" {
		query += " AND status = ?"
		args = append(args, string(status))
	}
	query += " ORDER BY created_at DESC"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*proto.ApprovalRequest
	for rows.Next() {
		a := &proto.ApprovalRequest{}
		var st, chID, payload, reviewerID, reviewNote sql.NullString
		var reviewedAt sql.NullInt64
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.AgentID, &chID, &a.Action, &payload,
			&st, &reviewerID, &reviewNote, &a.CreatedAt, &reviewedAt); err != nil {
			return nil, err
		}
		a.ChannelID = chID.String
		a.Payload = payload.String
		a.Status = proto.ApprovalStatus(st.String)
		a.ReviewerID = reviewerID.String
		a.ReviewNote = reviewNote.String
		a.ReviewedAt = reviewedAt.Int64
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateApproval(ctx context.Context, a *proto.ApprovalRequest) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE approval_requests SET status=?, reviewer_id=?, review_note=?, reviewed_at=? WHERE id=?`,
		string(a.Status), a.ReviewerID, a.ReviewNote, a.ReviewedAt, a.ID)
	return err
}

// --- Helpers ---

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
