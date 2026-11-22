package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
	"vpn-probe/internal/model"
)

type Store struct {
	db *sql.DB
}

func NewStore(dbPath string) (*Store, error) {
	if dbPath == "" {
		dbPath = "./data/vpn-probe.db"
	}

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS probe_history (
		id TEXT PRIMARY KEY,
		original_url TEXT NOT NULL,
		name TEXT,
		protocol TEXT,
		is_self INTEGER,
		online INTEGER,
		latency_ms INTEGER,
		china_exit INTEGER,
		exit_ip TEXT,
		exit_country TEXT,
		category TEXT,
		status_text TEXT,
		error_msg TEXT,
		diagnostic_json TEXT,
		split_json TEXT,
		tested_at DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_category_tested ON probe_history(category, tested_at);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// 动态兼容已有数据库结构
	_, _ = s.db.Exec("ALTER TABLE probe_history ADD COLUMN status_text TEXT")
	_, _ = s.db.Exec("ALTER TABLE probe_history ADD COLUMN diagnostic_json TEXT")
	return nil
}

// SaveResult 保存一条检测记录
func (s *Store) SaveResult(ctx context.Context, res *model.NodeProbeResult) error {
	query := `
	INSERT INTO probe_history (
		id, original_url, name, protocol, is_self, online, latency_ms,
		china_exit, exit_ip, exit_country, category, status_text, error_msg, diagnostic_json, split_json, tested_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	isSelfInt := 0
	if res.IsSelf {
		isSelfInt = 1
	}
	onlineInt := 0
	if res.Online {
		onlineInt = 1
	}
	chinaExitInt := 0
	if res.ChinaExit {
		chinaExitInt = 1
	}

	var splitJSON string
	if res.Split != nil {
		b, _ := json.Marshal(res.Split)
		splitJSON = string(b)
	}

	var diagJSON string
	if res.Diagnostic != nil {
		b, _ := json.Marshal(res.Diagnostic)
		diagJSON = string(b)
	}

	_, err := s.db.ExecContext(ctx, query,
		res.ID,
		res.OriginalURL,
		res.Name,
		res.Protocol,
		isSelfInt,
		onlineInt,
		res.LatencyMs,
		chinaExitInt,
		res.ExitIP,
		res.ExitCountry,
		string(res.Category),
		res.StatusText,
		res.ErrorMsg,
		diagJSON,
		splitJSON,
		res.TestedAt.Format(time.RFC3339),
	)
	return err
}

// GetRecentResults 查询最近的测试记录
func (s *Store) GetRecentResults(ctx context.Context, limit int) ([]*model.NodeProbeResult, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
	SELECT id, original_url, name, protocol, is_self, online, latency_ms,
	       china_exit, exit_ip, exit_country, category, COALESCE(status_text, ''), error_msg,
	       COALESCE(diagnostic_json, ''), COALESCE(split_json, ''), tested_at
	FROM probe_history
	ORDER BY tested_at DESC
	LIMIT ?
	`
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]*model.NodeProbeResult, 0)
	for rows.Next() {
		var r model.NodeProbeResult
		var isSelfInt, onlineInt, chinaExitInt int
		var catStr, statusText, diagJSON, splitJSON, testedAtStr string

		err := rows.Scan(
			&r.ID,
			&r.OriginalURL,
			&r.Name,
			&r.Protocol,
			&isSelfInt,
			&onlineInt,
			&r.LatencyMs,
			&chinaExitInt,
			&r.ExitIP,
			&r.ExitCountry,
			&catStr,
			&statusText,
			&r.ErrorMsg,
			&diagJSON,
			&splitJSON,
			&testedAtStr,
		)
		if err != nil {
			return nil, err
		}

		r.IsSelf = isSelfInt == 1
		r.Online = onlineInt == 1
		r.ChinaExit = chinaExitInt == 1
		r.Category = model.Category(catStr)
		r.StatusText = statusText
		r.TestedAt, _ = time.Parse(time.RFC3339, testedAtStr)

		if diagJSON != "" {
			var diag model.DiagnosticInfo
			if json.Unmarshal([]byte(diagJSON), &diag) == nil {
				r.Diagnostic = &diag
			}
		}

		if splitJSON != "" {
			var sp model.SplitInfo
			if json.Unmarshal([]byte(splitJSON), &sp) == nil {
				r.Split = &sp
			}
		}

		list = append(list, &r)
	}
	return list, nil
}
