package substore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"vpn-probe/internal/logger"
)

// ScriptOperatorInfo 单个算子脚本节点信息
type ScriptOperatorInfo struct {
	ID         string            `json:"id"`
	CustomName string            `json:"custom_name"`
	URL        string            `json:"url"`
	ScriptFile string            `json:"script_file"`
	Version    string            `json:"version"`
	Disabled   bool              `json:"disabled"`
	Arguments  map[string]string `json:"arguments,omitempty"`
}

// ShareLinkInfo 分享订阅文件信息
type ShareLinkInfo struct {
	Name             string               `json:"name"`
	DisplayName      string               `json:"display_name"`
	ShareURL         string               `json:"share_url"`
	ShareURLDomain1  string               `json:"share_url_domain1"`
	ShareURLDomain2  string               `json:"share_url_domain2"`
	SubURLDomain1    string               `json:"sub_url_domain1"`
	SubURLDomain2    string               `json:"sub_url_domain2"`
	ShareURLs        map[string]string    `json:"share_urls"`
	SubURLs          map[string]string    `json:"sub_urls"`
	FastSnapshotPath string               `json:"fast_snapshot_path,omitempty"` // 极速直出快照相对路径 (毫秒级响应，Issue #21)
	FastSnapshotURLs map[string]string    `json:"fast_snapshot_urls,omitempty"` // 极速直出快照全量链接
	TargetFormat     string               `json:"target_format"`
	SourceCollection string               `json:"source_collection"`
	PipelineScripts  []ScriptOperatorInfo `json:"pipeline_scripts"`
}

// ScriptFileInfo 脚本源代码与描述信息
type ScriptFileInfo struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Size        int    `json:"size"`
	Content     string `json:"content"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// SubStoreManifest SubStore 全景元数据（分享链接、插入脚本、脚本源码与同步状态）
type SubStoreManifest struct {
	Status        string                    `json:"status"`
	SubStoreURL   string                    `json:"substore_url"`
	Username      string                    `json:"username"`
	Domains       []string                  `json:"domains"`
	ShareLinks    []ShareLinkInfo           `json:"share_links"`
	Scripts       map[string]ScriptFileInfo `json:"scripts"`
	ScriptList    []string                  `json:"script_list"`
	TotalShares   int                       `json:"total_shares"`
	TotalScripts  int                       `json:"total_scripts"`
	SyncMechanism string                    `json:"sync_mechanism"`
	UpdatedAt     string                    `json:"updated_at"`
}

// GetManifest 获取 Sub-Store 所有分享订阅、插入的脚本管道及脚本完整源码
func (c *Client) GetManifest(ctx context.Context) (*SubStoreManifest, error) {
	cfg := c.GetConfig()
	if cfg == nil || !cfg.Enabled || cfg.URL == "" {
		return nil, fmt.Errorf("substore integration is not enabled")
	}

	baseURL := strings.TrimRight(cfg.URL, "/")
	filesURL := fmt.Sprintf("%s/api/files", baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, filesURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create files request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch substore files: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch files returned %d: %s", resp.StatusCode, string(b))
	}

	var filesResp struct {
		Status string           `json:"status"`
		Data   []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&filesResp); err != nil {
		return nil, fmt.Errorf("decode files response: %w", err)
	}

	domains := []string{"free.1992004.xyz", "sub.ashanzzz.com"}
	if len(cfg.Domains) >= 2 {
		domains = cfg.Domains
	}

	manifest := &SubStoreManifest{
		Status:        "ok",
		SubStoreURL:   baseURL,
		Username:      cfg.Username,
		Domains:       domains,
		ShareLinks:    make([]ShareLinkInfo, 0),
		Scripts:       make(map[string]ScriptFileInfo),
		ScriptList:    make([]string, 0),
		SyncMechanism: "方案B五维策略组架构: ashan-transit (线路机) + 区域家宽全家桶 (ashan-us-resi, ashan-jp-resi, ashan-other) + ashan-china-leak (送中隔离) -> ashan-master / Q-汇总 -> ashan / Q 订阅文件",
		UpdatedAt:     time.Now().Format("2006-01-02 15:04:05"),
	}

	token := "13602004317"
	if cfg.ShareToken != "" {
		token = cfg.ShareToken
	}

	scriptSet := make(map[string]bool)

	for _, item := range filesResp.Data {
		name, _ := item["name"].(string)
		if name == "" {
			continue
		}
		displayName, _ := item["displayName"].(string)
		sourceName, _ := item["sourceName"].(string)
		targetType, _ := item["type"].(string)

		escapedName := url.PathEscape(name)
		shareURL1 := fmt.Sprintf("https://%s/share/file/%s?token=%s", domains[0], escapedName, token)
		shareURL2 := fmt.Sprintf("https://%s/share/file/%s?token=%s", domains[1], escapedName, token)
		subURL1 := fmt.Sprintf("https://%s:%s@%s/%s/api/file/%s", cfg.Username, cfg.Password, domains[0], cfg.Password, escapedName)
		subURL2 := fmt.Sprintf("https://%s:%s@%s/%s/api/file/%s", cfg.Username, cfg.Password, domains[1], cfg.Password, escapedName)

		var ops []ScriptOperatorInfo
		if procList, ok := item["process"].([]any); ok {
			for _, p := range procList {
				pMap, ok := p.(map[string]any)
				if !ok {
					continue
				}
				id, _ := pMap["id"].(string)
				customName, _ := pMap["customName"].(string)
				disabled, _ := pMap["disabled"].(bool)

				var scriptURL string
				var argsMap map[string]string
				if args, ok := pMap["args"].(map[string]any); ok {
					scriptURL, _ = args["content"].(string)
					if am, ok := args["arguments"].(map[string]any); ok {
						argsMap = make(map[string]string)
						for k, v := range am {
							argsMap[k] = fmt.Sprint(v)
						}
					}
				}

				if scriptURL != "" {
					scriptFile := extractScriptFileName(scriptURL)
					version := extractScriptVersion(scriptURL)
					ops = append(ops, ScriptOperatorInfo{
						ID:         id,
						CustomName: customName,
						URL:        scriptURL,
						ScriptFile: scriptFile,
						Version:    version,
						Disabled:   disabled,
						Arguments:  argsMap,
					})
					scriptSet[scriptFile] = true
				}
			}
		}

		shareURLs := map[string]string{
			"free.1992004.xyz": shareURL1,
			"sub.ashanzzz.com": shareURL2,
		}
		subURLs := map[string]string{
			"free.1992004.xyz": subURL1,
			"sub.ashanzzz.com": subURL2,
		}

		fastPath := fmt.Sprintf("/api/sub/file/%s?token=%s", escapedName, token)
		fastURLs := map[string]string{
			"free.1992004.xyz": fmt.Sprintf("https://%s/api/sub/file/%s?token=%s", domains[0], escapedName, token),
			"sub.ashanzzz.com": fmt.Sprintf("https://%s/api/sub/file/%s?token=%s", domains[1], escapedName, token),
			"local_gateway":   fmt.Sprintf("http://192.168.8.11:33335/api/sub/file/%s?token=%s", escapedName, token),
		}

		manifest.ShareLinks = append(manifest.ShareLinks, ShareLinkInfo{
			Name:             name,
			DisplayName:      displayName,
			ShareURL:         shareURL1,
			ShareURLDomain1:  shareURL1,
			ShareURLDomain2:  shareURL2,
			SubURLDomain1:    subURL1,
			SubURLDomain2:    subURL2,
			ShareURLs:        shareURLs,
			SubURLs:          subURLs,
			FastSnapshotPath: fastPath,
			FastSnapshotURLs: fastURLs,
			TargetFormat:     targetType,
			SourceCollection: sourceName,
			PipelineScripts:  ops,
		})
	}

	manifest.TotalShares = len(manifest.ShareLinks)

	// 并发快速拉取引用的脚本内容 (单脚本设置 3 秒超时与安全容错，防止 CDN 丢包阻塞整体 Manifest 返回)
	type scriptRes struct {
		name    string
		content string
		err     error
	}

	var validScriptNames []string
	for sName := range scriptSet {
		if sName != "" {
			validScriptNames = append(validScriptNames, sName)
		}
	}

	resChan := make(chan scriptRes, len(validScriptNames))
	for _, sName := range validScriptNames {
		go func(name string) {
			sCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			content, err := c.GetScriptContent(sCtx, name)
			resChan <- scriptRes{name: name, content: content, err: err}
		}(sName)
	}

	for range validScriptNames {
		res := <-resChan
		content := res.content
		if res.err != nil {
			logger.Warnf("Fetch script content for %s: %v", res.name, res.err)
			content = fmt.Sprintf("// Script %s (remote fetch timeout/fallback: %v)", res.name, res.err)
		}

		desc := getScriptDescription(res.name)
		sURL := fmt.Sprintf("https://free.1992004.xyz/%s", res.name)
		manifest.Scripts[res.name] = ScriptFileInfo{
			Name:        res.name,
			URL:         sURL,
			Description: desc,
			Size:        len(content),
			Content:     content,
			UpdatedAt:   time.Now().Format("2006-01-02 15:04:05"),
		}
		manifest.ScriptList = append(manifest.ScriptList, res.name)
	}
	manifest.TotalScripts = len(manifest.ScriptList)

	return manifest, nil
}

// GetScriptContent 读取指定脚本的源代码
func (c *Client) GetScriptContent(ctx context.Context, scriptName string) (string, error) {
	scriptURL := fmt.Sprintf("https://free.1992004.xyz/%s", url.PathEscape(scriptName))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scriptURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %d fetching %s", resp.StatusCode, scriptURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// UpdateScript 热修改脚本源码，通过 SSH 上传到 Tokyo IIJ Web 根目录，并更新 Sub-Store 缓存版本号
func (c *Client) UpdateScript(ctx context.Context, scriptName string, newContent string) error {
	if scriptName == "" || strings.Contains(scriptName, "/") || strings.Contains(scriptName, "\\") {
		return fmt.Errorf("invalid script name: %s", scriptName)
	}

	// 1. 通过 SSH 写入 Web 根目录 (通过环境变量安全注入，禁止硬编码凭据与生产IP)
	sshHost := os.Getenv("SUBSTORE_SSH_HOST")
	if sshHost == "" {
		return fmt.Errorf("SUBSTORE_SSH_HOST environment variable is not configured; refusing remote script upload")
	}
	sshUser := os.Getenv("SUBSTORE_SSH_USER")
	if sshUser == "" {
		sshUser = "root"
	}
	sshPass := os.Getenv("SUBSTORE_SSH_PASS")
	if sshPass == "" {
		return fmt.Errorf("SUBSTORE_SSH_PASS environment variable is not configured; refusing remote script upload")
	}
	remotePath := fmt.Sprintf("/opt/1panel/www/sites/free.1992004.xyz/index/%s", scriptName)
	backupPath := fmt.Sprintf("%s.bak", remotePath)

	var hostKeyCallback ssh.HostKeyCallback
	if os.Getenv("SUBSTORE_SSH_INSECURE_HOSTKEY") == "true" {
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	} else {
		hostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			expectedFP := os.Getenv("SUBSTORE_SSH_FINGERPRINT")
			if expectedFP != "" {
				actualFP := ssh.FingerprintSHA256(key)
				if actualFP != expectedFP {
					return fmt.Errorf("ssh host key mismatch: expected %s, got %s", expectedFP, actualFP)
				}
			}
			return nil
		}
	}

	sshCfg := &ssh.ClientConfig{
		User:            sshUser,
		Auth:            []ssh.AuthMethod{ssh.Password(sshPass)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", sshHost, sshCfg)
	if err != nil {
		return fmt.Errorf("dial ssh %s: %w", sshHost, err)
	}
	defer client.Close()

	// 备份原文件
	bkSession, err := client.NewSession()
	if err == nil {
		_ = bkSession.Run(fmt.Sprintf("cp -f %s %s 2>/dev/null || true", remotePath, backupPath))
		bkSession.Close()
	}

	// 写入新脚本内容
	writeSession, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("create write session: %w", err)
	}
	defer writeSession.Close()

	writeSession.Stdin = strings.NewReader(newContent)
	writeCmd := fmt.Sprintf("cat > %s", remotePath)
	if err := writeSession.Run(writeCmd); err != nil {
		return fmt.Errorf("write remote file %s: %w", remotePath, err)
	}

	logger.Sync("[SUBSTORE] ✅ Updated script %s on Tokyo IIJ (%d bytes)", scriptName, len(newContent))

	// 2. 更新 Sub-Store 文件配置中的版本号 (Cache Busting)
	newVer := fmt.Sprintf("v=%d", time.Now().Unix())
	if err := c.bustScriptCacheInFiles(ctx, scriptName, newVer); err != nil {
		logger.Warnf("[SUBSTORE] Bust script cache failed: %v", err)
	}

	return nil
}

func (c *Client) bustScriptCacheInFiles(ctx context.Context, scriptName, newVer string) error {
	cfg := c.GetConfig()
	if cfg == nil || cfg.URL == "" {
		return fmt.Errorf("substore URL is empty")
	}
	baseURL := strings.TrimRight(cfg.URL, "/")
	filesURL := fmt.Sprintf("%s/api/files", baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, filesURL, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var filesResp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&filesResp); err != nil {
		return err
	}

	for _, fileItem := range filesResp.Data {
		fileName, _ := fileItem["name"].(string)
		if fileName == "" {
			continue
		}

		changed := false
		if procList, ok := fileItem["process"].([]any); ok {
			for _, p := range procList {
				pMap, ok := p.(map[string]any)
				if !ok {
					continue
				}
				args, ok := pMap["args"].(map[string]any)
				if !ok {
					continue
				}
				cStr, _ := args["content"].(string)
				if strings.Contains(cStr, scriptName) {
					// 替换或添加版本号
					basePart := strings.Split(cStr, "?")[0]
					hashPart := ""
					if strings.Contains(cStr, "#") {
						hashPart = "#" + strings.Split(cStr, "#")[1]
					}
					args["content"] = fmt.Sprintf("%s?%s%s", basePart, newVer, hashPart)
					changed = true
				}
			}
		}

		if changed {
			patchURL := fmt.Sprintf("%s/api/file/%s", baseURL, url.PathEscape(fileName))
			bodyBytes, _ := json.Marshal(fileItem)
			pReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, patchURL, bytes.NewReader(bodyBytes))
			if err == nil {
				c.setAuth(pReq)
				pReq.Header.Set("Content-Type", "application/json")
				if pResp, err := c.httpClient.Do(pReq); err == nil {
					pResp.Body.Close()
					logger.Sync("[SUBSTORE] Updated file %s with new script version %s", fileName, newVer)
				}
			}
		}
	}

	return nil
}

func extractScriptFileName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		parts := strings.Split(rawURL, "/")
		last := parts[len(parts)-1]
		return strings.Split(strings.Split(last, "?")[0], "#")[0]
	}
	return path.Base(u.Path)
}

func extractScriptVersion(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("v")
}

func getScriptDescription(name string) string {
	switch name {
	case "ashan_patch.js":
		return "阿山核心增量补丁：动态注入 dialer-proxy 链式中继、开启 Xray v26+ X25519MLKEM768、注入局域网直连与全量防送中规则。"
	case "convert_openclash_v2.js":
		return "OpenClash / Mihomo 转换算子：生成标准代理分组（选择代理、自动优选、日韩、美洲等）与分流规则链。"
	case "classify_songzhong.js":
		return "送中与不送中双轨分类算子：自动过滤并归类纯净节点与送中节点。"
	default:
		return "Sub-Store 订阅管道处理自定义 JavaScript 算子。"
	}
}
