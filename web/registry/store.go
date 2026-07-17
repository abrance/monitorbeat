package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Store 管理 agent 注册心跳数据的 SQLite 存储。
type Store struct {
	db  *sql.DB
	ttl time.Duration
}

// NewStore 使用已有的 *sql.DB 初始化 registry store。
// 调用方负责 db 的生命周期；Store 只做 migrate。
func NewStore(db *sql.DB, ttl time.Duration) (*Store, error) {
	s := &Store{db: db, ttl: ttl}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("registry migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			hostname   TEXT PRIMARY KEY,
			version    TEXT NOT NULL DEFAULT '',
			tasks      TEXT NOT NULL DEFAULT '',
			ip         TEXT NOT NULL DEFAULT '',
			k8s_node   TEXT NOT NULL DEFAULT '',
			start_time INTEGER NOT NULL DEFAULT 0,
			last_seen  INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_agents_last_seen ON agents(last_seen);
	`)
	return err
}

// Heartbeat 接收一次 agent 心跳：存在则更新，不存在则插入。
func (s *Store) Heartbeat(ctx context.Context, info AgentInfo) error {
	tasksJSON, err := json.Marshal(info.Tasks)
	if err != nil {
		return fmt.Errorf("marshal tasks: %w", err)
	}
	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO agents (hostname, version, tasks, ip, k8s_node, start_time, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(hostname) DO UPDATE SET
			version    = excluded.version,
			tasks      = excluded.tasks,
			ip         = excluded.ip,
			k8s_node   = excluded.k8s_node,
			start_time = excluded.start_time,
			last_seen  = excluded.last_seen
	`, info.Hostname, info.Version, string(tasksJSON),
		info.IP, info.K8sNode, info.StartTime, now)
	return err
}

// ListAgents 返回所有已知 agent，含 online 状态。
// 若 onlineOnly 为 true，仅返回 last_seen 在 TTL 内的 agent。
func (s *Store) ListAgents(ctx context.Context, onlineOnly bool) ([]AgentInfo, error) {
	cutoff := time.Now().Unix() - int64(s.ttl.Seconds())

	query := `SELECT hostname, version, tasks, ip, k8s_node, start_time, last_seen
			  FROM agents ORDER BY hostname`
	args := []any{}

	if onlineOnly {
		query = `SELECT hostname, version, tasks, ip, k8s_node, start_time, last_seen
				 FROM agents WHERE last_seen >= ? ORDER BY hostname`
		args = append(args, cutoff)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AgentInfo, 0)
	for rows.Next() {
		var a AgentInfo
		var tasksJSON string
		if err := rows.Scan(&a.Hostname, &a.Version, &tasksJSON,
			&a.IP, &a.K8sNode, &a.StartTime, &a.LastSeen); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tasksJSON), &a.Tasks)
		a.Online = a.LastSeen >= cutoff
		out = append(out, a)
	}
	return out, rows.Err()
}

// CleanStale 删除超过 TTL 未上报的 agent 记录。
func (s *Store) CleanStale(ctx context.Context) error {
	cutoff := time.Now().Unix() - int64(s.ttl.Seconds())
	_, err := s.db.ExecContext(ctx, `DELETE FROM agents WHERE last_seen < ?`, cutoff)
	return err
}

// CleanLoop 后台定期清理过期 agent。
func (s *Store) CleanLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.CleanStale(ctx)
		}
	}
}
