package logger

import (
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

type Logger struct {
	mu          sync.Mutex
	level       Level
	out         io.Writer
	file        *os.File
	filePath    string
	maxSize     int64 // 轮转阈值 (默认 10MB)
	maxBackups  int   // 保留旧日志数量 (默认 5)
	auditFile   *os.File
	auditMu     sync.Mutex
	stdLogger   *log.Logger
}

var globalLogger *Logger
var once sync.Once

// 常见节点链接与凭证脱敏正则
var (
	uuidRegex       = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	queryTokenRegex = regexp.MustCompile(`(?i)(token|password|auth|secret|key)=([^&]+)`)
	urlUserInfoRegex = regexp.MustCompile(`://([^:@]+)(:([^@]+))?@`)
)

// MaskURL 对代理链接或带有凭证的 URL 进行彻底脱敏
func MaskURL(raw string) string {
	if raw == "" {
		return ""
	}

	// 1. 如果包含标准 URL 结构，进行结构化脱敏
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err == nil && u.User != nil {
			if _, hasPass := u.User.Password(); hasPass {
				u.User = url.UserPassword(u.User.Username(), "******")
			} else {
				u.User = url.User("******")
			}
			// 脱敏 Query 中的敏感参数
			q := u.Query()
			for k := range q {
				lower := strings.ToLower(k)
				if strings.Contains(lower, "token") || strings.Contains(lower, "pass") || strings.Contains(lower, "secret") {
					q.Set(k, "******")
				}
			}
			u.RawQuery = q.Encode()
			return u.String()
		}
	}

	// 2. 正则兜底脱敏
	res := urlUserInfoRegex.ReplaceAllString(raw, "://******@")
	res = uuidRegex.ReplaceAllString(res, "********-****-****-****-************")
	res = queryTokenRegex.ReplaceAllString(res, "$1=******")
	return res
}

// MaskSecret 对单值密码或 Token 进行脱敏 (保留前后各1位，中间替换为***)
func MaskSecret(secret string) string {
	if len(secret) == 0 {
		return ""
	}
	if len(secret) <= 3 {
		return "******"
	}
	return secret[:1] + "******" + secret[len(secret)-1:]
}

func Init(levelStr, logFilePath string) (*Logger, error) {
	var lvl Level
	switch strings.ToLower(levelStr) {
	case "debug":
		lvl = LevelDebug
	case "warn":
		lvl = LevelWarn
	case "error":
		lvl = LevelError
	default:
		lvl = LevelInfo
	}

	writers := []io.Writer{os.Stdout}
	var f *os.File
	var auditF *os.File

	if logFilePath != "" {
		dir := filepath.Dir(logFilePath)
		if err := os.MkdirAll(dir, 0755); err == nil {
			file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err == nil {
				f = file
				writers = append(writers, file)
			} else {
				fmt.Printf("[WARN] Failed to open log file %s: %v\n", logFilePath, err)
			}

			// 初始化专用审计日志 audit.log
			auditPath := filepath.Join(dir, "audit.log")
			af, aErr := os.OpenFile(auditPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if aErr == nil {
				auditF = af
			}
		}
	}

	multi := io.MultiWriter(writers...)
	l := &Logger{
		level:      lvl,
		out:        multi,
		file:       f,
		filePath:   logFilePath,
		maxSize:    10 * 1024 * 1024, // 10 MB
		maxBackups: 5,
		auditFile:  auditF,
		stdLogger:  log.New(multi, "", 0),
	}

	once.Do(func() {
		globalLogger = l
	})

	return l, nil
}

func Get() *Logger {
	if globalLogger == nil {
		l, _ := Init("info", "./data/vpn-probe.log")
		return l
	}
	return globalLogger
}

// checkRotate 检查是否需要轮转主日志文件
func (l *Logger) checkRotate() {
	if l.file == nil || l.filePath == "" {
		return
	}
	fi, err := l.file.Stat()
	if err != nil || fi.Size() < l.maxSize {
		return
	}

	// 执行轮转
	_ = l.file.Close()

	for i := l.maxBackups - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", l.filePath, i)
		newPath := fmt.Sprintf("%s.%d", l.filePath, i+1)
		_ = os.Rename(oldPath, newPath)
	}
	_ = os.Rename(l.filePath, l.filePath+".1")

	newFile, err := os.OpenFile(l.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		l.file = newFile
		writers := []io.Writer{os.Stdout, newFile}
		multi := io.MultiWriter(writers...)
		l.out = multi
		l.stdLogger = log.New(multi, "", 0)
	}
}

// Event 记录单行结构化日志: 2026-11-20 23:01:02.123 | INFO | component | action | details
func (l *Logger) Event(lvl Level, component, action, details string) {
	if lvl < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.checkRotate()

	now := time.Now().Format("2006-01-02 15:04:05.000")
	cleanDetails := MaskURL(details)
	line := fmt.Sprintf("%s | %-5s | %-10s | %-12s | %s", now, lvl.String(), component, action, cleanDetails)
	_ = l.stdLogger.Output(2, line)
}

// Audit 记录高危敏感操作审计 (独立写入 audit.log，只追加不覆盖)
func (l *Logger) Audit(action, actor, target, result, detail string) {
	l.auditMu.Lock()
	defer l.auditMu.Unlock()

	now := time.Now().Format("2006-01-02 15:04:05.000")
	cleanDetail := MaskURL(detail)
	auditLine := fmt.Sprintf("%s | AUDIT | actor=%s action=%s target=%s result=%s detail=%s\n",
		now, actor, action, target, result, cleanDetail)

	if l.auditFile != nil {
		_, _ = l.auditFile.WriteString(auditLine)
		_ = l.auditFile.Sync()
	}

	// 同时在控制台和主日志打一条 INFO 记录
	l.Event(LevelInfo, "audit", action, fmt.Sprintf("actor=%s target=%s result=%s", actor, target, result))
}

// 格式化输出兼容方法 (全部内置自动 URL/凭据脱敏)
func (l *Logger) logf(lvl Level, component, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.Event(lvl, component, "log", msg)
}

func (l *Logger) Debugf(format string, args ...any) {
	l.logf(LevelDebug, "system", format, args...)
}

func (l *Logger) Infof(format string, args ...any) {
	l.logf(LevelInfo, "system", format, args...)
}

func (l *Logger) Warnf(format string, args ...any) {
	l.logf(LevelWarn, "system", format, args...)
}

func (l *Logger) Errorf(format string, args ...any) {
	l.logf(LevelError, "system", format, args...)
}

func (l *Logger) Probe(format string, args ...any) {
	l.logf(LevelInfo, "probe", format, args...)
}

func (l *Logger) Sync(format string, args ...any) {
	l.logf(LevelInfo, "substore", format, args...)
}

// 全局便捷调用
func Event(lvl Level, component, action, details string) { Get().Event(lvl, component, action, details) }
func Audit(action, actor, target, result, detail string)  { Get().Audit(action, actor, target, result, detail) }
func Debugf(format string, args ...any)                  { Get().Debugf(format, args...) }
func Infof(format string, args ...any)                   { Get().Infof(format, args...) }
func Warnf(format string, args ...any)                   { Get().Warnf(format, args...) }
func Errorf(format string, args ...any)                  { Get().Errorf(format, args...) }
func Probe(format string, args ...any)                   { Get().Probe(format, args...) }
func Sync(format string, args ...any)                    { Get().Sync(format, args...) }
