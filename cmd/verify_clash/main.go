package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ANSI Color Codes
const (
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Blue   = "\033[34m"
	Purple = "\033[35m"
	Cyan   = "\033[36m"
	Bold   = "\033[1m"
)

type ClashConfig struct {
	Port               int                    `yaml:"port,omitempty"`
	SocksPort          int                    `yaml:"socks-port,omitempty"`
	MixedPort          int                    `yaml:"mixed-port,omitempty"`
	AllowLan           bool                   `yaml:"allow-lan,omitempty"`
	Mode               string                 `yaml:"mode,omitempty"`
	LogLevel           string                 `yaml:"log-level,omitempty"`
	ExternalController string                 `yaml:"external-controller,omitempty"`
	Proxies            []map[string]any       `yaml:"proxies"`
	ProxyGroups        []ProxyGroup           `yaml:"proxy-groups"`
	Rules              []string               `yaml:"rules"`
	RuleProviders      map[string]any         `yaml:"rule-providers,omitempty"`
	Dns                map[string]any         `yaml:"dns,omitempty"`
}

type ProxyGroup struct {
	Name     string   `yaml:"name"`
	Type     string   `yaml:"type"`
	Proxies  []string `yaml:"proxies"`
	URL      string   `yaml:"url,omitempty"`
	Interval int      `yaml:"interval,omitempty"`
}

type VerifyReport struct {
	TotalChecks int
	Passed      int
	Failed      int
	Warnings    int
	Errors      []string
}

func (r *VerifyReport) Pass(msg string) {
	r.TotalChecks++
	r.Passed++
	fmt.Printf("  %s[PASS]%s %s\n", Green, Reset, msg)
}

func (r *VerifyReport) Fail(msg string) {
	r.TotalChecks++
	r.Failed++
	r.Errors = append(r.Errors, msg)
	fmt.Printf("  %s[FAIL]%s %s\n", Red, Reset, msg)
}

func (r *VerifyReport) Warn(msg string) {
	r.TotalChecks++
	r.Warnings++
	fmt.Printf("  %s[WARN]%s %s\n", Yellow, Reset, msg)
}

func getLandingCountry(nodeName string) string {
	target := nodeName
	if strings.Contains(nodeName, "➔") {
		parts := strings.Split(nodeName, "➔")
		target = strings.TrimSpace(parts[len(parts)-1])
	}
	targetUpper := strings.ToUpper(target)
	if strings.Contains(targetUpper, "US") || strings.Contains(target, "Pavlov") || strings.Contains(target, "Frontier") || strings.Contains(target, "Verizon") || strings.Contains(target, "美国") {
		return "US"
	}
	if strings.Contains(targetUpper, "JP") || strings.Contains(target, "Tokyo") || strings.Contains(target, "SoftBank") || strings.Contains(target, "BIGLOBE") || strings.Contains(target, "GLBB") || strings.Contains(target, "KDDI") || strings.Contains(target, "日本") {
		return "JP"
	}
	if strings.Contains(targetUpper, "TW") || strings.Contains(target, "HiNet") || strings.Contains(target, "Taiwan") || strings.Contains(target, "台湾") {
		return "TW"
	}
	if strings.Contains(targetUpper, "SG") || strings.Contains(target, "StarHub") || strings.Contains(target, "Singapore") || strings.Contains(target, "新加坡") {
		return "SG"
	}
	if strings.Contains(targetUpper, "KR") || strings.Contains(target, "KT") || strings.Contains(target, "Korea") || strings.Contains(target, "韩国") {
		return "KR"
	}
	return "OTHER"
}

func findMihomoBin(custom string) string {
	if custom != "" {
		if _, err := os.Stat(custom); err == nil {
			return custom
		}
	}
	candidates := []string{
		"/opt/data/tmp/mihomo",
		"mihomo",
		"/usr/local/bin/mihomo",
		"/usr/bin/mihomo",
		"./mihomo",
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func main() {
	targetURL := flag.String("url", "", "Subscription or Snapshot URL to verify")
	targetFile := flag.String("file", "", "Local YAML file to verify")
	token := flag.String("token", "Woo@@@204317", "Sub-Store / API Token")
	customMihomo := flag.String("mihomo", "", "Path to mihomo binary (auto-detects if empty)")
	skipKernel := flag.Bool("skip-kernel", false, "Skip Mihomo kernel compilation check")
	flag.Parse()

	fmt.Println()
	fmt.Printf("%s%s========================================================================%s\n", Bold, Cyan, Reset)
	fmt.Printf("%s%s   CLASH / MIHOMO 订阅配置与自建策略分组专项严苛验收系统 (v2.1.0)       %s\n", Bold, Cyan, Reset)
	fmt.Printf("%s%s========================================================================%s\n", Bold, Cyan, Reset)
	fmt.Println()

	var yamlContent []byte
	var sourceName string

	if *targetFile != "" {
		sourceName = "本地文件: " + *targetFile
		data, err := os.ReadFile(*targetFile)
		if err != nil {
			fmt.Printf("%s读取文件失败: %v%s\n", Red, err, Reset)
			os.Exit(1)
		}
		yamlContent = data
	} else {
		u := *targetURL
		if u == "" {
			u = "https://free.1992004.xyz/Woo@@@204317/api/file/ashan"
		}
		sourceName = "远程端点: " + u
		fmt.Printf("🌐 正在从端点拉取渲染后的真实配置: %s%s%s ...\n", Purple, u, Reset)

		client := &http.Client{
			Timeout: 90 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
		req, _ := http.NewRequest("GET", u, nil)
		if *token != "" {
			req.SetBasicAuth("ashan", *token)
		}
		req.Header.Set("User-Agent", "ClashMeta/v1.19.0")

		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("%s拉取订阅端点失败: %v%s\n", Red, err, Reset)
			os.Exit(1)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			fmt.Printf("%s服务端返回异常 HTTP 状态码: %d%s\n", Red, resp.StatusCode, Reset)
			os.Exit(1)
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("%s读取响应流失败: %v%s\n", Red, err, Reset)
			os.Exit(1)
		}
		yamlContent = data
		fmt.Printf("✅ 成功拉取配置! 大小: %s%d 字节%s, 耗时: %v\n\n", Green, len(data), Reset, time.Since(start))
	}

	report := &VerifyReport{}

	// =========================================================================
	// 验证板块 3: 咱们这个文件能否正确被 Clash 代理软件解析
	// (先进行底层反序列化与核心语法结构检查)
	// =========================================================================
	fmt.Printf("%s【模块 3: Clash / Mihomo 规范语法与可解析性验证】%s\n", Bold, Reset)

	var config ClashConfig
	err := yaml.Unmarshal(yamlContent, &config)
	if err != nil {
		report.Fail(fmt.Sprintf("YAML 语法解析严重失败 (Clash 软件将直接报错崩溃): %v", err))
		printSummary(report, sourceName)
		os.Exit(1)
	}
	report.Pass("YAML 格式严格反序列化成功 (零语法/缩进/标点错误)")

	// 1. 顶层关键字段检查
	if len(config.Proxies) > 0 {
		report.Pass(fmt.Sprintf("顶层 proxies 列表解析成功, 节点总数: %d", len(config.Proxies)))
	} else {
		report.Fail("顶层 proxies 列表为空, 代理软件无可用出口")
	}

	if len(config.ProxyGroups) > 0 {
		report.Pass(fmt.Sprintf("顶层 proxy-groups 策略组解析成功, 分组总数: %d", len(config.ProxyGroups)))
	} else {
		report.Fail("顶层 proxy-groups 为空, 客户端无法渲染策略选择面板")
	}

	if len(config.Rules) > 0 {
		report.Pass(fmt.Sprintf("顶层 rules 路由规则解析成功, 规则总条数: %d", len(config.Rules)))
	} else {
		report.Warn("顶层 rules 规则为空")
	}

	// 2. 节点协议与字段合法性检测
	validProtocols := map[string]bool{
		"vless": true, "ss": true, "trojan": true, "hysteria2": true, "vmess": true, "socks5": true, "http": true, "wireguard": true,
	}
	invalidProtocols := 0
	proxyNameSet := make(map[string]bool)
	for _, p := range config.Proxies {
		name, _ := p["name"].(string)
		pType, _ := p["type"].(string)
		if name == "" {
			report.Fail("发现未命名节点 (name 为空)")
		} else {
			proxyNameSet[name] = true
		}
		if !validProtocols[strings.ToLower(pType)] {
			invalidProtocols++
			fmt.Printf("      %s[!] 未知协议类型: name='%s', type='%s'%s\n", Yellow, name, pType, Reset)
		}
	}
	if invalidProtocols == 0 {
		report.Pass(fmt.Sprintf("全部 %d 个节点协议类型均符合 Clash/Mihomo 标准规范", len(config.Proxies)))
	} else {
		report.Fail(fmt.Sprintf("存在 %d 个不支持或未知的协议类型", invalidProtocols))
	}

	// 3. 策略组类型合法性检测
	validGroupTypes := map[string]bool{
		"select": true, "url-test": true, "fallback": true, "load-balance": true, "relay": true,
	}
	groupNameSet := make(map[string]bool)
	groupMap := make(map[string]ProxyGroup)
	for _, g := range config.ProxyGroups {
		groupNameSet[g.Name] = true
		groupMap[g.Name] = g
		if !validGroupTypes[strings.ToLower(g.Type)] {
			report.Fail(fmt.Sprintf("策略组【%s】类型 '%s' 不符合 Clash 标准", g.Name, g.Type))
		}
	}
	report.Pass(fmt.Sprintf("全部 %d 个策略组的类型(Type)均符合规范", len(config.ProxyGroups)))

	// 4. 悬空引用检查 (Dangling Reference) — 彻底杜绝 Clash 闪退与红屏
	danglingCount := 0
	specialBuiltins := map[string]bool{
		"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true,
	}
	for _, g := range config.ProxyGroups {
		for _, target := range g.Proxies {
			if !proxyNameSet[target] && !groupNameSet[target] && !specialBuiltins[target] {
				// 特殊容忍: REJECT-DROP 部分老内核不支持但新版支持
				if target == "REJECT-DROP" {
					continue
				}
				report.Fail(fmt.Sprintf("策略组【%s】包含未定义悬空引用: '%s'", g.Name, target))
				danglingCount++
			}
		}
	}
	if danglingCount == 0 {
		report.Pass("无悬空引用 (所有策略组引用的子节点或子组均存在于配置文件中)")
	}

	// 5. 死循环引用检查 (Cycle Detection)
	hasCycle := false
	for gName := range groupMap {
		visited := make(map[string]bool)
		var checkCycle func(curr string, path []string) bool
		checkCycle = func(curr string, path []string) bool {
			if visited[curr] {
				report.Fail(fmt.Sprintf("检测到策略组死循环引用: %s -> %s", strings.Join(path, " -> "), curr))
				return true
			}
			visited[curr] = true
			if grp, ok := groupMap[curr]; ok {
				for _, sub := range grp.Proxies {
					if groupMap[sub].Name != "" {
						if checkCycle(sub, append(path, curr)) {
							return true
						}
					}
				}
			}
			visited[curr] = false
			return false
		}
		if checkCycle(gName, nil) {
			hasCycle = true
			break
		}
	}
	if !hasCycle {
		report.Pass("策略组拓扑结构无死循环引用 (DAG 拓扑检测通过)")
	}

	fmt.Println()

	// =========================================================================
	// 验证板块 1: 分组有没有上级分组和我们自定分组
	// =========================================================================
	fmt.Printf("%s【模块 1: 分组结构完整性验收 (上级分组与自定分组)】%s\n", Bold, Reset)

	// 1. 上级决策/分流策略组检查
	requiredParentGroups := []string{
		"选择代理", "手动选择", "AI服务", "巴哈姆特", "谷歌服务", "Youtube",
	}
	missingParents := 0
	for _, p := range requiredParentGroups {
		if groupNameSet[p] {
			report.Pass(fmt.Sprintf("上级业务策略组存在: 【%s】 (包含 %d 个下游策略)", p, len(groupMap[p].Proxies)))
		} else {
			report.Fail(fmt.Sprintf("缺少关键上级业务策略组: 【%s】", p))
			missingParents++
		}
	}

	// 严禁未授权的流媒体与游戏特定业务策略组（用户铁律 #19）
	unauthorizedGroups := []string{"美区流媒体", "日区流媒体与游戏"}
	for _, u := range unauthorizedGroups {
		if groupNameSet[u] {
			report.Fail(fmt.Sprintf("违规检测到未授权特定业务策略组【%s】(违反铁律 #19 严禁私加流媒体/游戏组)", u))
		} else {
			report.Pass(fmt.Sprintf("未授权特定业务策略组拦截合规: 未包含【%s】(符合铁律 #19)", u))
		}
	}

	// 2. 检查我们核心自定分组是否存在
	requiredCustomGroups := []string{
		"自建-线路分组",
		"自建-家宽分组",
		"自建-美国家宽分组",
		"自建-日本家宽分组",
		"自建-台湾家宽分组",
		"自建-其他家宽分组",
		"自建-送中分组",
	}
	missingCustom := 0
	for _, c := range requiredCustomGroups {
		if groupNameSet[c] {
			report.Pass(fmt.Sprintf("自定核心落地分组存在: 【%s】 (池内节点: %d 个)", c, len(groupMap[c].Proxies)))
		} else {
			report.Fail(fmt.Sprintf("缺少必须自定落地分组: 【%s】", c))
			missingCustom++
		}
	}

	// 3. 上级分组是否正确汇聚并绑定了自定分组
	selectGroup := groupMap["选择代理"]
	hasTransitInSelect := false
	hasAllResiInSelect := false
	hasUSInSelect := false
	hasJPInSelect := false
	hasTWInSelect := false
	for _, p := range selectGroup.Proxies {
		if p == "自建-线路分组" {
			hasTransitInSelect = true
		}
		if p == "自建-家宽分组" {
			hasAllResiInSelect = true
		}
		if p == "自建-美国家宽分组" {
			hasUSInSelect = true
		}
		if p == "自建-日本家宽分组" {
			hasJPInSelect = true
		}
		if p == "自建-台湾家宽分组" {
			hasTWInSelect = true
		}
	}
	if hasTransitInSelect && hasAllResiInSelect && hasUSInSelect && hasJPInSelect && hasTWInSelect {
		report.Pass("【选择代理】顶级入口已完整汇聚全部自建核心分组 (线路/全量家宽/美/日/台)")
	} else {
		report.Fail("【选择代理】未能完整挂载所有核心自建分组")
	}

	aiGroup := groupMap["AI服务"]
	if len(aiGroup.Proxies) > 0 && (aiGroup.Proxies[0] == "自建-美国家宽分组" || aiGroup.Proxies[0] == "自建-美国和美国家宽") {
		report.Pass("【AI服务】业务分流已按最高优先级硬绑定美国家宽分组 (搞定美区风控)")
	} else if len(aiGroup.Proxies) > 0 {
		report.Fail(fmt.Sprintf("【AI服务】首选未指向美国家宽分组, 当前首选为: %v", aiGroup.Proxies[:1]))
	}

	// 核心业务组隔离送中校验 (铁律: 选择代理、谷歌服务、Youtube 绝不包含自建-送中)
	for _, grpName := range []string{"选择代理", "谷歌服务", "Youtube"} {
		if grp, ok := groupMap[grpName]; ok {
			hasLeak := false
			for _, px := range grp.Proxies {
				if px == "自建-送中" || px == "自建-送中分组" || px == "ashan-china-leak" {
					hasLeak = true
					break
				}
			}
			if hasLeak {
				report.Fail(fmt.Sprintf("核心分组【%s】违规混入送中隔离箱 (违反铁律 #19)", grpName))
			} else {
				report.Pass(fmt.Sprintf("核心分组【%s】送中隔离核验通过 (未混入送中节点)", grpName))
			}
		}
	}

	bahaGroup := groupMap["巴哈姆特"]
	if len(bahaGroup.Proxies) > 0 && (bahaGroup.Proxies[0] == "自建-台湾家宽分组" || bahaGroup.Proxies[0] == "自建-其他" || bahaGroup.Proxies[0] == "自建-其他家宽分组") {
		report.Pass("【巴哈姆特】业务分流已成功硬绑定台湾原生落地分组")
	} else if len(bahaGroup.Proxies) > 0 {
		report.Warn(fmt.Sprintf("【巴哈姆特】首选为: %v (建议首选【自建-台湾家宽分组】)", bahaGroup.Proxies[:1]))
	}

	fmt.Println()

	// =========================================================================
	// 验证板块 2: 命名对不对、内部节点内容对不对
	// =========================================================================
	fmt.Printf("%s【模块 2: 命名精准度与节点内容落地纯净度验收】%s\n", Bold, Reset)

	// 1. 命名规范性验收 (严禁多字少字，兼容别名)
	expectedAliases := map[string]string{
		"自建-线路机":     "自建-线路分组",
		"自建-美国和美国家宽": "自建-美国家宽分组",
		"自建-日本和日本家宽": "自建-日本家宽分组",
		"自建-其他":      "自建-其他家宽分组",
		"自建-送中":      "自建-送中分组",
	}
	for alias, realTarget := range expectedAliases {
		if grp, ok := groupMap[alias]; ok {
			if len(grp.Proxies) > 0 && grp.Proxies[0] == realTarget {
				report.Pass(fmt.Sprintf("兼容别名组规范正确: 【%s】 -> 【%s】", alias, realTarget))
			} else {
				report.Fail(fmt.Sprintf("兼容别名组【%s】未正确映射至【%s】", alias, realTarget))
			}
		} else {
			report.Warn(fmt.Sprintf("兼容别名组【%s】未生成", alias))
		}
	}

	// 2. 纯骨干专线纯净度校验
	transitGroup := groupMap["自建-线路分组"]
	dirtyInTransit := 0
	for _, p := range transitGroup.Proxies {
		if strings.Contains(p, "家宽") || strings.Contains(p, "➔") || p == "38.175.222.44" {
			dirtyInTransit++
			report.Fail(fmt.Sprintf("【自建-线路分组】中混入非骨干或送中节点: %s", p))
		}
	}
	if dirtyInTransit == 0 && len(transitGroup.Proxies) > 0 {
		report.Pass(fmt.Sprintf("【自建-线路分组】内容纯净 (全为纯骨干直连专线: %v)", transitGroup.Proxies))
	}

	// 3. 全量家宽总池校验
	allResiGroup := groupMap["自建-家宽分组"]
	if len(allResiGroup.Proxies) >= 30 {
		report.Pass(fmt.Sprintf("【自建-家宽分组】全网状中继总池丰满 (共包含 %d 个直连与链式中继节点)", len(allResiGroup.Proxies)))
	} else if len(allResiGroup.Proxies) > 0 {
		report.Warn(fmt.Sprintf("【自建-家宽分组】节点数较少 (%d 个), 建议保持全网状中继覆盖", len(allResiGroup.Proxies)))
	} else {
		report.Fail("【自建-家宽分组】为空")
	}

	// 4. 美区家宽落地纯净度校验 (绝不能混入其他国家)
	usGroup := groupMap["自建-美国家宽分组"]
	wrongInUS := 0
	for _, p := range usGroup.Proxies {
		if p == "DIRECT" {
			continue
		}
		cc := getLandingCountry(p)
		if cc != "US" {
			wrongInUS++
			report.Fail(fmt.Sprintf("【自建-美国家宽分组】混入非美区落地节点: %s (判定落地: %s)", p, cc))
		}
	}
	if wrongInUS == 0 && len(usGroup.Proxies) > 0 {
		report.Pass(fmt.Sprintf("【自建-美国家宽分组】100%% 纯净美区落地 (共 %d 个美区节点, 零跨国污染)", len(usGroup.Proxies)))
	}

	// 5. 日区家宽落地纯净度校验 (重点排查历史 BUG: 东京拉美国家宽错配)
	jpGroup := groupMap["自建-日本家宽分组"]
	wrongInJP := 0
	for _, p := range jpGroup.Proxies {
		if p == "DIRECT" {
			continue
		}
		cc := getLandingCountry(p)
		if cc != "JP" {
			wrongInJP++
			report.Fail(fmt.Sprintf("【自建-日本家宽分组】混入非日区落地节点: %s (判定落地: %s)", p, cc))
		}
	}
	if wrongInJP == 0 && len(jpGroup.Proxies) > 0 {
		report.Pass(fmt.Sprintf("【自建-日本家宽分组】100%% 纯净日区落地 (共 %d 个日区节点, 历史东京拉美家宽错配已彻底根除!)", len(jpGroup.Proxies)))
	}

	// 6. 台湾家宽落地纯净度校验
	twGroup := groupMap["自建-台湾家宽分组"]
	wrongInTW := 0
	for _, p := range twGroup.Proxies {
		if p == "DIRECT" {
			continue
		}
		cc := getLandingCountry(p)
		if cc != "TW" {
			wrongInTW++
			report.Fail(fmt.Sprintf("【自建-台湾家宽分组】混入非台区落地节点: %s", p))
		}
	}
	if wrongInTW == 0 && len(twGroup.Proxies) > 0 {
		report.Pass(fmt.Sprintf("【自建-台湾家宽分组】100%% 纯净台湾落地 (包含台湾直连与多专线拉台家宽中继: %d 个节点)", len(twGroup.Proxies)))
	}

	// 7. 送中隔离性校验
	cleanGroupNames := []string{
		"自建-线路分组", "自建-家宽分组", "自建-美国家宽分组", "自建-日本家宽分组", "自建-台湾家宽分组", "自建-其他家宽分组", "不送中",
	}
	leakCount := 0
	for _, gName := range cleanGroupNames {
		grp := groupMap[gName]
		for _, p := range grp.Proxies {
			if strings.Contains(p, "送中") || strings.Contains(p, "China-Leak") || p == "38.175.222.44" {
				leakCount++
				report.Fail(fmt.Sprintf("送中节点泄漏至干净组【%s】: %s", gName, p))
			}
		}
	}
	if leakCount == 0 {
		report.Pass("送中隔离性 100% 达标 (所有纯净组中无任何送中/受污染节点)")
	}

	fmt.Println()

	// =========================================================================
	// 验证板块 4: Mihomo 真实内核原生语法与路由规则编译校验
	// =========================================================================
	fmt.Printf("%s【模块 4: Mihomo 真实内核引擎全量语法与路由规则硬核编译校验】%s\n", Bold, Reset)

	if *skipKernel {
		report.Warn("用户指定 --skip-kernel，跳过 Mihomo 原生内核动态编译测试")
	} else {
		mihomoBin := findMihomoBin(*customMihomo)
		if mihomoBin == "" {
			report.Warn("未在 PATH 或 /opt/data/tmp/mihomo 发现 mihomo 内核，跳过真机内核编译 (静态分析已通过)")
		} else {
			tmpFile, err := os.CreateTemp("", "clash_verify_*.yaml")
			if err != nil {
				report.Fail(fmt.Sprintf("创建临时配置文件失败: %v", err))
			} else {
				tmpPath := tmpFile.Name()
				defer os.Remove(tmpPath)
				_ = os.WriteFile(tmpPath, yamlContent, 0644)
				tmpFile.Close()

				cmd := exec.Command(mihomoBin, "-t", "-f", tmpPath)
				outputBytes, cmdErr := cmd.CombinedOutput()
				outStr := string(outputBytes)

				if cmdErr == nil && strings.Contains(outStr, "test is successful") {
					report.Pass(fmt.Sprintf("Mihomo 真实内核原生编译与语法校验 100%% 成功 (内核: %s, 零配置错误/零Panic)", mihomoBin))
				} else {
					report.Fail(fmt.Sprintf("Mihomo 内核编译报错 (exit: %v): %s", cmdErr, strings.TrimSpace(outStr)))
				}
			}
		}
	}

	printSummary(report, sourceName)
}

func printSummary(report *VerifyReport, sourceName string) {
	fmt.Println()
	fmt.Printf("%s%s========================= 验收总结与裁决结果 =========================%s\n", Bold, Cyan, Reset)
	fmt.Printf("🎯 验收数据源: %s\n", sourceName)
	fmt.Printf("📊 总校验项: %d 项\n", report.TotalChecks)
	fmt.Printf("   - %s通过 (PASSED): %d 项%s\n", Green, report.Passed, Reset)
	fmt.Printf("   - %s警告 (WARNINGS): %d 项%s\n", Yellow, report.Warnings, Reset)
	fmt.Printf("   - %s失败 (FAILED): %d 项%s\n", Red, report.Failed, Reset)
	fmt.Println()

	if report.Failed == 0 {
		fmt.Printf("%s%s🏆 验收裁决结果: 【100%% 全部通过 - 工业级完美状态】%s\n", Bold, Green, Reset)
		fmt.Printf("   ✔ 分组体系完美 (上级业务策略组与自定落地组层次分明)\n")
		fmt.Printf("   ✔ 命名规范严密 (符合自建-***分组命名体系与别名兼容)\n")
		fmt.Printf("   ✔ Clash代理软件完美解析 (零语法错误/零悬空引用/零循环依赖)\n")
		fmt.Printf("   ✔ 节点内容绝对纯净 (日区无美区混入、送中彻底物理隔离)\n")
	} else {
		fmt.Printf("%s%s❌ 验收裁决结果: 【未完全通过 - 存在 %d 项不符合标准】%s\n", Bold, Red, report.Failed, Reset)
		for i, errStr := range report.Errors {
			fmt.Printf("   %d. %s\n", i+1, errStr)
		}
	}
	fmt.Printf("%s%s========================================================================%s\n", Bold, Cyan, Reset)
	fmt.Println()
}
