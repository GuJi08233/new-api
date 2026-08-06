package service

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// IP 归属地查询：按配置顺序依次尝试 gitee / ipwho.is / ip9，结果统一为
// {continent, country, province, city, district, latitude, longitude, postal, asn, org, isp}
// 并写入 ip_infos 表缓存，同一个 IP 只查询一次外部接口。

var ipLocationHTTPClient = &http.Client{Timeout: 8 * time.Second}

// continentZh 把 ipwho.is 返回的英文大洲名统一为中文。
var continentZh = map[string]string{
	"Asia":          "亚洲",
	"Europe":        "欧洲",
	"Africa":        "非洲",
	"North America": "北美洲",
	"South America": "南美洲",
	"Oceania":       "大洋洲",
	"Antarctica":    "南极洲",
}

// countryCodeContinentZh 供 ip9(无大洲字段)按国家代码推导大洲，覆盖常见地区。
var countryCodeContinentZh = map[string]string{
	"cn": "亚洲", "hk": "亚洲", "mo": "亚洲", "tw": "亚洲", "jp": "亚洲", "kr": "亚洲",
	"sg": "亚洲", "my": "亚洲", "th": "亚洲", "vn": "亚洲", "ph": "亚洲", "id": "亚洲",
	"in": "亚洲", "pk": "亚洲", "bd": "亚洲", "ae": "亚洲", "sa": "亚洲", "il": "亚洲",
	"tr": "亚洲", "kz": "亚洲", "la": "亚洲", "kh": "亚洲", "mm": "亚洲", "np": "亚洲",
	"lk": "亚洲", "mn": "亚洲", "kp": "亚洲", "ir": "亚洲", "iq": "亚洲", "qa": "亚洲",
	"us": "北美洲", "ca": "北美洲", "mx": "北美洲",
	"br": "南美洲", "ar": "南美洲", "cl": "南美洲", "co": "南美洲", "pe": "南美洲",
	"gb": "欧洲", "de": "欧洲", "fr": "欧洲", "it": "欧洲", "es": "欧洲", "nl": "欧洲",
	"ru": "欧洲", "pl": "欧洲", "se": "欧洲", "no": "欧洲", "fi": "欧洲", "dk": "欧洲",
	"ch": "欧洲", "at": "欧洲", "be": "欧洲", "ie": "欧洲", "pt": "欧洲", "cz": "欧洲",
	"ua": "欧洲", "ro": "欧洲", "hu": "欧洲", "gr": "欧洲", "bg": "欧洲",
	"au": "大洋洲", "nz": "大洋洲",
	"za": "非洲", "eg": "非洲", "ng": "非洲", "ke": "非洲", "ma": "非洲", "dz": "非洲",
}

// LookupIpInfo 返回某 IP 的归属地信息，优先读数据库缓存，未命中时按配置
// 顺序调用外部接口并落库。
func LookupIpInfo(ip string) (*model.IpInfo, error) {
	ip = strings.TrimSpace(ip)
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return nil, errors.New("invalid ip")
	}
	ip = parsed.String()

	if cached, err := model.GetIpInfo(ip); err == nil {
		return cached, nil
	}
	generation, generationErr := model.GetIpInfoCacheGeneration()
	if generationErr != nil {
		common.SysError("failed to read ip info cache generation: " + generationErr.Error())
	}

	setting := operation_setting.GetIpLocationSetting()
	var order []string
	isIpv4 := parsed.To4() != nil
	if isIpv4 {
		order = setting.ResolvedIpv4Order()
	} else {
		order = setting.ResolvedIpv6Order()
	}

	var lastErr error
	for _, provider := range order {
		info, err := queryIpLocationProvider(provider, ip, isIpv4, setting)
		if err != nil {
			lastErr = err
			continue
		}
		info.Provider = provider
		if generationErr == nil {
			if _, err := model.SaveIpInfo(info, generation); err != nil {
				common.SysError("failed to cache ip info for " + ip + ": " + err.Error())
			}
		}
		return info, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no ip location provider configured")
	}
	return nil, fmt.Errorf("all ip location providers failed: %w", lastErr)
}

func queryIpLocationProvider(provider string, ip string, isIpv4 bool, setting *operation_setting.IpLocationSetting) (*model.IpInfo, error) {
	switch provider {
	case operation_setting.IpLocationProviderGitee:
		// gitee 接口不支持 IPv6，直接跳过以免浪费一次必然失败的请求。
		if !isIpv4 {
			return nil, errors.New("gitee provider does not support ipv6")
		}
		return queryGiteeIpLocation(ip, setting.GiteeApiKey)
	case operation_setting.IpLocationProviderIpwhois:
		return queryIpwhoisLocation(ip)
	case operation_setting.IpLocationProviderIp9:
		return queryIp9Location(ip)
	default:
		return nil, fmt.Errorf("unknown ip location provider: %s", provider)
	}
}

func ipLocationRequest(req *http.Request) ([]byte, error) {
	resp, err := ipLocationHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// queryGiteeIpLocation 调用 gitee AI 的 IP 归属地接口，其返回即目标统一格式。
func queryGiteeIpLocation(ip string, apiKey string) (*model.IpInfo, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("gitee api key is not configured")
	}
	payload, err := common.Marshal(map[string]string{"ip": ip})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, "https://ai.gitee.com/v1/ip/location", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))

	body, err := ipLocationRequest(req)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Ip        string   `json:"ip"`
		Continent string   `json:"continent"`
		Country   string   `json:"country"`
		Province  string   `json:"province"`
		City      string   `json:"city"`
		District  string   `json:"district"`
		Isp       string   `json:"isp"`
		Lat       *float64 `json:"lat"`
		Lon       *float64 `json:"lon"`
		Error     *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := common.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.Error != nil {
		return nil, errors.New("gitee: " + parsed.Error.Message)
	}
	if parsed.Country == "" {
		return nil, errors.New("gitee: empty response")
	}
	return &model.IpInfo{
		Ip:        ip,
		Continent: parsed.Continent,
		Country:   parsed.Country,
		Province:  parsed.Province,
		City:      parsed.City,
		District:  parsed.District,
		Latitude:  formatGeoCoord(parsed.Lat),
		Longitude: formatGeoCoord(parsed.Lon),
		Isp:       parsed.Isp,
	}, nil
}

// queryIpwhoisLocation 调用 ipwho.is，v4/v6 通吃；大洲翻译为中文，其余字段
// 保留接口返回的英文。
func queryIpwhoisLocation(ip string) (*model.IpInfo, error) {
	req, err := http.NewRequest(http.MethodGet, "https://ipwho.is/"+ip, nil)
	if err != nil {
		return nil, err
	}
	body, err := ipLocationRequest(req)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Success    bool     `json:"success"`
		Message    string   `json:"message"`
		Continent  string   `json:"continent"`
		Country    string   `json:"country"`
		Region     string   `json:"region"`
		City       string   `json:"city"`
		Postal     string   `json:"postal"`
		Latitude   *float64 `json:"latitude"`
		Longitude  *float64 `json:"longitude"`
		Connection struct {
			Asn int    `json:"asn"`
			Org string `json:"org"`
			Isp string `json:"isp"`
		} `json:"connection"`
	}
	if err := common.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if !parsed.Success {
		return nil, errors.New("ipwho.is: " + parsed.Message)
	}

	continent := parsed.Continent
	if zh, ok := continentZh[continent]; ok {
		continent = zh
	}
	isp := parsed.Connection.Isp
	if isp == "" {
		isp = parsed.Connection.Org
	}
	asn := ""
	if parsed.Connection.Asn != 0 {
		asn = strconv.Itoa(parsed.Connection.Asn)
	}
	return &model.IpInfo{
		Ip:        ip,
		Continent: continent,
		Country:   parsed.Country,
		Province:  parsed.Region,
		City:      parsed.City,
		Postal:    parsed.Postal,
		Latitude:  formatGeoCoord(parsed.Latitude),
		Longitude: formatGeoCoord(parsed.Longitude),
		Asn:       asn,
		Org:       parsed.Connection.Org,
		Isp:       isp,
	}, nil
}

// queryIp9Location 调用 ip9.com.cn，省市区运营商全中文；接口无大洲字段，
// 按国家代码推导。
func queryIp9Location(ip string) (*model.IpInfo, error) {
	req, err := http.NewRequest(http.MethodGet, "https://ip9.com.cn/get?ip="+ip, nil)
	if err != nil {
		return nil, err
	}
	body, err := ipLocationRequest(req)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Ret  int `json:"ret"`
		Data struct {
			Country     string `json:"country"`
			CountryCode string `json:"country_code"`
			Prov        string `json:"prov"`
			City        string `json:"city"`
			Isp         string `json:"isp"`
			Lat         string `json:"lat"`
			Lng         string `json:"lng"`
			PostCode    string `json:"post_code"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.Ret != 200 || parsed.Data.Country == "" {
		return nil, fmt.Errorf("ip9: ret %d", parsed.Ret)
	}
	// ip9 的经纬度是字符串（如 "113.28"），空值或解析失败视为缺失。
	latitude, longitude := "", ""
	if f, err := strconv.ParseFloat(parsed.Data.Lat, 64); err == nil {
		latitude = formatGeoCoord(&f)
	}
	if f, err := strconv.ParseFloat(parsed.Data.Lng, 64); err == nil {
		longitude = formatGeoCoord(&f)
	}
	return &model.IpInfo{
		Ip:        ip,
		Continent: countryCodeContinentZh[strings.ToLower(parsed.Data.CountryCode)],
		Country:   parsed.Data.Country,
		Province:  parsed.Data.Prov,
		City:      parsed.Data.City,
		Postal:    parsed.Data.PostCode,
		Latitude:  latitude,
		Longitude: longitude,
		Isp:       parsed.Data.Isp,
	}, nil
}

// formatGeoCoord 把可选经纬度转为保留 5 位小数的字符串，避免落库 float
// 精度差异与 GORM 跨库类型不一致。nil 表示上游未返回，合法的 0 坐标会保留。
func formatGeoCoord(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', 5, 64)
}
