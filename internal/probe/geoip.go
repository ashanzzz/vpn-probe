package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"vpn-probe/internal/model"
)

// IPGeoStore 定义 IP 缓存持久化接口，避免 probe 与 store 产生紧耦合循环依赖
type IPGeoStore interface {
	GetCachedIPGeo(ctx context.Context, ip string, maxAge time.Duration) (*model.IPGeoRecord, error)
	SaveCachedIPGeo(ctx context.Context, item *model.IPGeoRecord) error
	DeleteCachedIPGeo(ctx context.Context, ip string) error
}

var (
	globalIPStore   IPGeoStore
	globalIPStoreMu sync.RWMutex
)

// SetGlobalIPGeoStore 设置全局 IP 持久化存储接口
func SetGlobalIPGeoStore(s IPGeoStore) {
	globalIPStoreMu.Lock()
	globalIPStore = s
	globalIPStoreMu.Unlock()
}

// InvalidateIPGeoCache 强制失效指定 IP 缓存 (L1 内存 + L2 数据库)
func InvalidateIPGeoCache(ctx context.Context, ip string) {
	trimmed := strings.TrimSpace(ip)
	if trimmed == "" {
		return
	}
	geoCacheLock.Lock()
	delete(geoCache, trimmed)
	geoCacheLock.Unlock()

	globalIPStoreMu.RLock()
	st := globalIPStore
	globalIPStoreMu.RUnlock()
	if st != nil {
		_ = st.DeleteCachedIPGeo(ctx, trimmed)
	}
}

// GeoResult 表示 IP 归属地与网络组织信息
type GeoResult struct {
	IP          string    `json:"ip"`
	City        string    `json:"city"`
	Region      string    `json:"region"`
	Country     string    `json:"country"`
	CountryCode string    `json:"country_code"`
	ISP         string    `json:"isp"`
	Org         string    `json:"org"`
	IPType      string    `json:"ip_type"` // "家宽" | "机房"
	Source      string    `json:"source"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

// DetermineIPType 根据 IP、ISP、组织信息以及节点属性综合判定 IP 属性 (家宽 vs 机房)
func DetermineIPType(ip, isp, org string, isResidentialNode bool) string {
	trimmedIP := strings.TrimSpace(ip)
	if trimmedIP == "" {
		if isResidentialNode {
			return "家宽"
		}
		return "机房"
	}

	// 1. 特殊 IP 段: Cloudflare WARP / Edge IP 段 (104.28.*, 104.29.*, 162.158.*, 172.64.* 等) 属于机房/CDN
	if strings.HasPrefix(trimmedIP, "104.28.") || strings.HasPrefix(trimmedIP, "104.29.") ||
		strings.HasPrefix(trimmedIP, "162.158.") || strings.HasPrefix(trimmedIP, "172.64.") ||
		strings.HasPrefix(trimmedIP, "172.65.") || strings.HasPrefix(trimmedIP, "172.66.") ||
		strings.HasPrefix(trimmedIP, "172.67.") || strings.HasPrefix(trimmedIP, "172.68.") ||
		strings.HasPrefix(trimmedIP, "172.69.") || strings.HasPrefix(trimmedIP, "172.70.") ||
		strings.HasPrefix(trimmedIP, "172.71.") || strings.HasPrefix(trimmedIP, "198.41.") {
		return "机房"
	}

	text := strings.ToLower(isp + " " + org)

	// 2. 明确的数据中心/云厂商/托管机房关键词 (机房)
	datacenterKeywords := []string{
		"cloudflare", "amazon", "aws", "google", "microsoft", "azure", "digitalocean",
		"linode", "akamai", "fastly", "vultr", "hetzner", "ovh", "oracle", "alibaba",
		"aliyun", "tencent", "hosting", "datacenter", "data center", "server", "leaseweb",
		"choopa", "m247", "zenlayer", "cogent", "hurricane", "he.net", "psychz",
		"quadranet", "constant", "colocrossing", "anexia", "ucloud", "qcloud",
		"huawei", "contabo", "layeronline", "serverius", "hostkey", "webnx",
	}
	for _, kw := range datacenterKeywords {
		if strings.Contains(text, kw) {
			return "机房"
		}
	}

	// 3. 明确的住宅/民用宽带/本地电信运营商关键词 (家宽)
	residentialKeywords := []string{
		"frontier", "verizon", "comcast", "at&t", "charter", "spectrum", "cox",
		"centurylink", "lumen", "optimum", "xfinity", "kddi", "softbank",
		"iij", "sonet", "so-net", "hkbn", "hgc", "pccw", "netvigator",
		"chunghwa", "hinet", "kbro", "china telecom", "china unicom", "china mobile",
		"broadband", "telecom", "cable", "fiber", "ftth", "residential", "dialup",
		"consumer", "home telecom", "biglobe", "glbb", "pavlov", "starhub", "singtel",
		"m1", "ocn", "ntt", "uplus", "lg u+", "sk broadband", "korea telecom",
	}
	for _, kw := range residentialKeywords {
		if strings.Contains(text, kw) {
			return "家宽"
		}
	}

	// 针对 "kt" 进行安全的分词匹配，避免包含 kt 子串的机房词产生假阳性
	for _, word := range strings.Fields(text) {
		cleanWord := strings.Trim(word, ",.-_()[]\"'")
		if cleanWord == "kt" {
			return "家宽"
		}
	}

	// 4. 若已知为家宽节点，且非明确机房
	if isResidentialNode {
		return "家宽"
	}

	// 5. 默认归类为机房
	return "机房"
}

var (
	geoCacheLock sync.RWMutex
	geoCache     = make(map[string]GeoResult)
)

// IATACityMap 常见 Cloudflare 机房/机场代码 (colo) 对应城市与国家对照表
var IATACityMap = map[string][2]string{
	// 美国 / 北美
	"SJC": {"San Jose", "US"},
	"LAX": {"Los Angeles", "US"},
	"SFO": {"San Francisco", "US"},
	"SEA": {"Seattle", "US"},
	"ORD": {"Chicago", "US"},
	"DFW": {"Dallas", "US"},
	"EWR": {"Newark", "US"},
	"JFK": {"New York", "US"},
	"IAD": {"Washington", "US"},
	"MIA": {"Miami", "US"},
	"ATL": {"Atlanta", "US"},
	"PHX": {"Phoenix", "US"},
	"DEN": {"Denver", "US"},
	"YVR": {"Vancouver", "CA"},
	"YYZ": {"Toronto", "CA"},

	// 日本
	"NRT": {"Tokyo", "JP"},
	"HND": {"Tokyo", "JP"},
	"KIX": {"Osaka", "JP"},
	"ITM": {"Osaka", "JP"},
	"NGO": {"Nagoya", "JP"},
	"CTS": {"Sapporo", "JP"},
	"SDJ": {"Sendai", "JP"},
	"FUK": {"Fukuoka", "JP"},
	"OKA": {"Okinawa", "JP"},

	// 亚太主要地区
	"HKG": {"Hong Kong", "HK"},
	"TPE": {"Taipei", "TW"},
	"KHH": {"Kaohsiung", "TW"},
	"SIN": {"Singapore", "SG"},
	"ICN": {"Seoul", "KR"},
	"GMP": {"Seoul", "KR"},
	"BKK": {"Bangkok", "TH"},
	"KUL": {"Kuala Lumpur", "MY"},
	"MNL": {"Manila", "PH"},
	"CGK": {"Jakarta", "ID"},
	"SYD": {"Sydney", "AU"},
	"MEL": {"Melbourne", "AU"},
	"BNE": {"Brisbane", "AU"},
	"AKL": {"Auckland", "NZ"},

	// 北美主流枢纽扩充
	"PDX": {"Portland", "US"},
	"SMF": {"Sacramento", "US"},
	"LAS": {"Las Vegas", "US"},
	"SLC": {"Salt Lake City", "US"},
	"IAH": {"Houston", "US"},
	"BOS": {"Boston", "US"},
	"PHL": {"Philadelphia", "US"},
	"CLT": {"Charlotte", "US"},
	"DTW": {"Detroit", "US"},
	"MSP": {"Minneapolis", "US"},
	"MCI": {"Kansas City", "US"},
	"STL": {"St. Louis", "US"},
	"TPA": {"Tampa", "US"},
	"MCO": {"Orlando", "US"},
	"SNA": {"Santa Ana", "US"},
	"SAN": {"San Diego", "US"},

	// 欧洲主要城市
	"LHR": {"London", "GB"},
	"FRA": {"Frankfurt", "DE"},
	"AMS": {"Amsterdam", "NL"},
	"CDG": {"Paris", "FR"},
	"MAD": {"Madrid", "ES"},
	"MXP": {"Milan", "IT"},
	"ZRH": {"Zurich", "CH"},
	"ARN": {"Stockholm", "SE"},
	"HEL": {"Helsinki", "FI"},
	"DUB": {"Dublin", "IE"},
}

// LookupGeo 解析 IP 归属的具体城市、国家与运营商（带本地内存缓存与 CF colo 机场码兜底）
func LookupGeo(ctx context.Context, client *http.Client, ip, colo string) GeoResult {
	trimmedIP := strings.TrimSpace(ip)
	if trimmedIP == "" {
		if client != nil {
			var selfGeo GeoResult
			if err := queryIPSB(ctx, client, "", &selfGeo); err == nil && selfGeo.IP != "" {
				selfGeo.Source = "ip.sb (self)"
				selfGeo.IPType = DetermineIPType(selfGeo.IP, selfGeo.ISP, selfGeo.Org, false)
				selfGeo.UpdatedAt = time.Now()
				return selfGeo
			}
		}
		if city, ok := IATACityMap[strings.ToUpper(colo)]; ok {
			return GeoResult{
				City:        city[0],
				CountryCode: city[1],
				Source:      "cf_colo",
			}
		}
		return GeoResult{}
	}

	// 1. 查 L1 内存缓存
	geoCacheLock.RLock()
	cached, ok := geoCache[trimmedIP]
	geoCacheLock.RUnlock()
	if ok && cached.City != "" {
		return cached
	}

	// 2. 查 L2 SQLite 持久化数据库 (如果已注入 store)
	globalIPStoreMu.RLock()
	st := globalIPStore
	globalIPStoreMu.RUnlock()

	if st != nil && trimmedIP != "" {
		// 先以 365天查询，如果查出是家宽且超过 180天则认为过期
		if rec, err := st.GetCachedIPGeo(ctx, trimmedIP, 365*24*time.Hour); err == nil && rec != nil && rec.City != "" {
			isExpired := false
			if rec.IPType == "家宽" && time.Since(rec.UpdatedAt) > 180*24*time.Hour {
				isExpired = true
			}
			if !isExpired {
				result := GeoResult{
					IP:          rec.IP,
					City:        rec.City,
					Region:      rec.Region,
					Country:     rec.Country,
					CountryCode: rec.CountryCode,
					ISP:         rec.ISP,
					Org:         rec.Org,
					IPType:      rec.IPType,
					Source:      rec.Source + " (sqlite_hit)",
					UpdatedAt:   rec.UpdatedAt,
				}
				// 回填到 L1 内存
				geoCacheLock.Lock()
				geoCache[trimmedIP] = result
				geoCacheLock.Unlock()
				return result
			}
		}
	}

	httpClient := client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 3 * time.Second}
	}

	var res GeoResult
	res.IP = trimmedIP

	// 3. 尝试从 api.ip.sb/geoip 查询 (高可靠海外 GeoIP 服务)
	if err := queryIPSB(ctx, httpClient, trimmedIP, &res); err == nil && res.City != "" {
		res.Source = "ip.sb"
	} else {
		// 4. 备用尝试从 ipwho.is 查询
		if err := queryIPWhois(ctx, httpClient, trimmedIP, &res); err == nil && res.City != "" {
			res.Source = "ipwho.is"
		}
	}

	// 5. 若在线查询失败或城市为空，使用 Cloudflare colo 兜底
	if res.City == "" && colo != "" {
		if mapping, found := IATACityMap[strings.ToUpper(colo)]; found {
			res.City = mapping[0]
			if res.CountryCode == "" {
				res.CountryCode = mapping[1]
			}
			res.Source = "cf_colo_fallback"
		}
	}

	// 6. 协商判定 IP 属性 (家宽 vs 机房)
	res.IPType = DetermineIPType(res.IP, res.ISP, res.Org, false)
	res.UpdatedAt = time.Now()

	// 7. 写入 L1 内存缓存与 L2 SQLite 持久化数据库
	if res.City != "" || res.CountryCode != "" {
		geoCacheLock.Lock()
		geoCache[trimmedIP] = res
		geoCacheLock.Unlock()

		if st != nil && res.IP != "" {
			_ = st.SaveCachedIPGeo(ctx, &model.IPGeoRecord{
				IP:          res.IP,
				City:        res.City,
				Region:      res.Region,
				Country:     res.Country,
				CountryCode: res.CountryCode,
				ISP:         res.ISP,
				Org:         res.Org,
				IPType:      res.IPType,
				Source:      res.Source,
				UpdatedAt:   res.UpdatedAt,
			})
		}
	}

	return res
}

func queryIPSB(ctx context.Context, client *http.Client, ip string, out *GeoResult) error {
	reqURL := fmt.Sprintf("https://api.ip.sb/geoip/%s", ip)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ip.sb status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var d struct {
		IP          string `json:"ip"`
		City        string `json:"city"`
		Region      string `json:"region"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		ISP         string `json:"isp"`
		Org         string `json:"organization"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return err
	}

	if d.IP != "" {
		out.IP = d.IP
	}
	out.City = d.City
	out.Region = d.Region
	out.Country = d.Country
	out.CountryCode = d.CountryCode
	out.ISP = d.ISP
	out.Org = d.Org
	return nil
}

func queryIPWhois(ctx context.Context, client *http.Client, ip string, out *GeoResult) error {
	reqURL := fmt.Sprintf("https://ipwho.is/%s", ip)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ipwhois status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var d struct {
		Success     bool   `json:"success"`
		IP          string `json:"ip"`
		City        string `json:"city"`
		Region      string `json:"region"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		Connection  struct {
			ISP string `json:"isp"`
			Org string `json:"org"`
		} `json:"connection"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return err
	}
	if !d.Success {
		return fmt.Errorf("ipwhois returned success=false")
	}

	if d.IP != "" {
		out.IP = d.IP
	}
	out.City = d.City
	out.Region = d.Region
	out.Country = d.Country
	out.CountryCode = d.CountryCode
	out.ISP = d.Connection.ISP
	out.Org = d.Connection.Org
	return nil
}
