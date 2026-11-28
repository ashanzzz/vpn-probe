package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	CREATE TABLE IF NOT EXISTS speed_history (
		id TEXT PRIMARY KEY,
		node_name TEXT NOT NULL,
		tested_at DATETIME NOT NULL,
		type TEXT NOT NULL DEFAULT 'manual',
		speed_mb_s REAL NOT NULL DEFAULT 0,
		speed_mbps REAL NOT NULL DEFAULT 0,
		ttfb_ms INTEGER NOT NULL DEFAULT 0,
		latency_ms INTEGER NOT NULL DEFAULT 0,
		relay_name TEXT,
		file_size_mb INTEGER NOT NULL DEFAULT 10,
		score REAL
	);
	CREATE INDEX IF NOT EXISTS idx_speed_history_node ON speed_history(node_name, tested_at DESC);

	CREATE TABLE IF NOT EXISTS ip_geo_cache (
		ip TEXT PRIMARY KEY,
		city TEXT,
		region TEXT,
		country TEXT,
		country_code TEXT,
		isp TEXT,
		org TEXT,
		ip_type TEXT,
		source TEXT,
		updated_at DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_ip_geo_updated ON ip_geo_cache(updated_at);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// 动态兼容已有数据库结构
	_, _ = s.db.Exec("ALTER TABLE probe_history ADD COLUMN status_text TEXT")
	_, _ = s.db.Exec("ALTER TABLE probe_history ADD COLUMN diagnostic_json TEXT")
	_, _ = s.db.Exec("ALTER TABLE probe_history ADD COLUMN exit_city TEXT")
	_, _ = s.db.Exec("ALTER TABLE probe_history ADD COLUMN exit_isp TEXT")
	_, _ = s.db.Exec("ALTER TABLE probe_history ADD COLUMN exit_ip_type TEXT")
	// speed_history 表通过 CREATE TABLE IF NOT EXISTS 确保存在

	// 初始化动态多选选项卡与嵌套节点组数据表及默认预设
	return s.InitTabTables()
}

// GetCachedIPGeo 获取指定 IP 的缓存归属信息，如果超过 maxAge 则返回 nil
func (s *Store) GetCachedIPGeo(ctx context.Context, ip string, maxAge time.Duration) (*model.IPGeoRecord, error) {
	query := `
	SELECT ip, city, region, country, country_code, isp, org, ip_type, source, updated_at
	FROM ip_geo_cache
	WHERE ip = ?
	`
	var item model.IPGeoRecord
	var updatedStr string
	err := s.db.QueryRowContext(ctx, query, strings.TrimSpace(ip)).Scan(
		&item.IP,
		&item.City,
		&item.Region,
		&item.Country,
		&item.CountryCode,
		&item.ISP,
		&item.Org,
		&item.IPType,
		&item.Source,
		&updatedStr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	t, err := time.Parse(time.RFC3339, updatedStr)
	if err == nil {
		item.UpdatedAt = t
	}
	if maxAge > 0 && time.Since(item.UpdatedAt) > maxAge {
		return nil, nil // 已超期，需重新联网查询
	}
	return &item, nil
}

// SaveCachedIPGeo 保存或更新 IP 的归属与家宽/机房属性
func (s *Store) SaveCachedIPGeo(ctx context.Context, item *model.IPGeoRecord) error {
	if item == nil || strings.TrimSpace(item.IP) == "" {
		return nil
	}
	query := `
	INSERT INTO ip_geo_cache (
		ip, city, region, country, country_code, isp, org, ip_type, source, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(ip) DO UPDATE SET
		city = excluded.city,
		region = excluded.region,
		country = excluded.country,
		country_code = excluded.country_code,
		isp = excluded.isp,
		org = excluded.org,
		ip_type = excluded.ip_type,
		source = excluded.source,
		updated_at = excluded.updated_at
	`
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx, query,
		strings.TrimSpace(item.IP),
		item.City,
		item.Region,
		item.Country,
		item.CountryCode,
		item.ISP,
		item.Org,
		item.IPType,
		item.Source,
		item.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

// DeleteCachedIPGeo 强制清除指定 IP 的缓存 (用于前端/用户主动刷新)
func (s *Store) DeleteCachedIPGeo(ctx context.Context, ip string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM ip_geo_cache WHERE ip = ?", strings.TrimSpace(ip))
	return err
}

// SaveResult 保存一条检测记录
func (s *Store) SaveResult(ctx context.Context, res *model.NodeProbeResult) error {
	query := `
	INSERT INTO probe_history (
		id, original_url, name, protocol, is_self, online, latency_ms,
		china_exit, exit_ip, exit_country, exit_city, exit_isp, exit_ip_type, category, status_text, error_msg, diagnostic_json, split_json, tested_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		res.ExitCity,
		res.ExitISP,
		res.ExitIPType,
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
	       china_exit, exit_ip, exit_country, COALESCE(exit_city, ''), COALESCE(exit_isp, ''), COALESCE(exit_ip_type, ''),
	       category, COALESCE(status_text, ''), error_msg,
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
			&r.ExitCity,
			&r.ExitISP,
			&r.ExitIPType,
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

// SpeedHistoryItem 单条测速历史记录 (用于前端展示)
type SpeedHistoryItem struct {
	ID         string  `json:"id"`
	NodeName   string  `json:"node_name"`
	TestedAt   string  `json:"tested_at"`
	Type       string  `json:"type"`
	SpeedMBs   float64 `json:"speed_mb_s"`
	SpeedMbps  float64 `json:"speed_mbps"`
	TTFBMs     int     `json:"ttfb_ms"`
	LatencyMs  int     `json:"latency_ms"`
	RelayName  string  `json:"relay_name,omitempty"`
	FileSizeMB int     `json:"file_size_mb"`
	Score      float64 `json:"score,omitempty"`
}

// SaveSpeedHistory 保存一条测速历史记录
func (s *Store) SaveSpeedHistory(ctx context.Context, nodeName string, speedType string, speed *model.SpeedResult, latencyMs int) error {
	if speed == nil {
		return nil
	}
	id := fmt.Sprintf("%s-%d", nodeName, time.Now().UnixNano())
	testedAt := speed.TestedAt
	if testedAt.IsZero() {
		testedAt = time.Now()
	}
	if speedType == "" {
		speedType = "manual"
	}
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO speed_history (id, node_name, tested_at, type, speed_mb_s, speed_mbps, ttfb_ms, latency_ms, file_size_mb, score)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, 10, ?)`,
		id,
		nodeName,
		testedAt.Format(time.RFC3339),
		speedType,
		speed.SpeedMBps,
		speed.SpeedMbps,
		speed.TTFBMs,
		latencyMs,
		speed.Score,
	)
	return err
}

// GetSpeedHistory 查询指定节点最近 N 条测速历史 (降序)
func (s *Store) GetSpeedHistory(ctx context.Context, nodeName string, limit int) ([]*SpeedHistoryItem, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, `
	SELECT id, node_name, tested_at, type, speed_mb_s, speed_mbps, ttfb_ms, latency_ms, COALESCE(relay_name,''), file_size_mb, COALESCE(score, 0)
	FROM speed_history
	WHERE node_name = ?
	ORDER BY tested_at DESC
	LIMIT ?`, nodeName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*SpeedHistoryItem
	for rows.Next() {
		var item SpeedHistoryItem
		if err := rows.Scan(
			&item.ID, &item.NodeName, &item.TestedAt, &item.Type,
			&item.SpeedMBs, &item.SpeedMbps, &item.TTFBMs, &item.LatencyMs,
			&item.RelayName, &item.FileSizeMB, &item.Score,
		); err != nil {
			return nil, err
		}
		list = append(list, &item)
	}
	return list, nil
}
