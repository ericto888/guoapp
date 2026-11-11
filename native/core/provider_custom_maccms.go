package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 自定义源：用户在应用内录入苹果CMS 采集站点，运行时注册成动态站源。
// 源 ID 以 custom: 前缀区分内置站源，持久化在应用数据目录的 custom_sources.json。

const customSourcePrefix = "custom:"

const customSourceMaxBytes = 256 * 1024

const customSourceImportLimit = 200

const customSourceMaxNameRunes = 40

type customMaccmsSource struct {
	ID string `json:"id"`
	// Name 展示名称。
	Name string `json:"name"`
	// Base 用户填写的站点或采集接口地址（已规整）。
	Base string `json:"base"`
	// API 探测成功后缓存的采集接口地址，为空时按 Base 逐个候选探测。
	API       string    `json:"api,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
}

type customMaccmsRegistry struct {
	mu      sync.RWMutex
	path    string
	sources map[string]customMaccmsSource
}

func newCustomMaccmsRegistry(directory string) *customMaccmsRegistry {
	return &customMaccmsRegistry{
		path:    filepath.Join(directory, "custom_sources.json"),
		sources: map[string]customMaccmsSource{},
	}
}

func (registry *customMaccmsRegistry) load() {
	if registry == nil {
		return
	}
	body, err := os.ReadFile(registry.path)
	if err != nil || len(body) > customSourceMaxBytes {
		return
	}
	var records []customMaccmsSource
	if json.Unmarshal(body, &records) != nil {
		return
	}
	registry.mu.Lock()
	registry.sources = map[string]customMaccmsSource{}
	for _, record := range records {
		record.ID = strings.TrimSpace(record.ID)
		record.Base = strings.TrimRight(strings.TrimSpace(record.Base), "/")
		record.API = strings.TrimRight(strings.TrimSpace(record.API), "/")
		record.Name = strings.TrimSpace(record.Name)
		if !validCustomMaccmsRecord(record) {
			continue
		}
		if _, exists := registry.sources[record.ID]; exists {
			continue
		}
		registry.sources[record.ID] = record
	}
	registry.mu.Unlock()
}

// save 调用方必须持有写锁。
func (registry *customMaccmsRegistry) save() error {
	records := make([]customMaccmsSource, 0, len(registry.sources))
	for _, record := range registry.sources {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	body, err := json.Marshal(records)
	if err != nil || len(body) > customSourceMaxBytes {
		return err
	}
	return writeNativeCacheFile(registry.path, body)
}

func (registry *customMaccmsRegistry) all() []customMaccmsSource {
	if registry == nil {
		return nil
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	records := make([]customMaccmsSource, 0, len(registry.sources))
	for _, record := range registry.sources {
		records = append(records, record)
	}
	return records
}

func (registry *customMaccmsRegistry) get(id string) (customMaccmsSource, bool) {
	if registry == nil {
		return customMaccmsSource{}, false
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	record, found := registry.sources[id]
	return record, found
}

func (registry *customMaccmsRegistry) put(record customMaccmsSource) error {
	if registry == nil {
		return errors.New("自定义源尚未就绪")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.sources[record.ID] = record
	return registry.save()
}

func (registry *customMaccmsRegistry) remove(id string) error {
	if registry == nil {
		return errors.New("自定义源尚未就绪")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, found := registry.sources[id]; !found {
		return errors.New("该自定义源不存在")
	}
	delete(registry.sources, id)
	return registry.save()
}

func (registry *customMaccmsRegistry) rememberAPI(id, api string) {
	if registry == nil {
		return
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	record, found := registry.sources[id]
	if !found || record.API == api {
		return
	}
	record.API = api
	registry.sources[id] = record
	_ = registry.save()
}

func validCustomMaccmsRecord(record customMaccmsSource) bool {
	if record.ID == "" || record.Name == "" || record.Base == "" {
		return false
	}
	if !strings.HasPrefix(record.ID, customSourcePrefix) {
		return false
	}
	parsed, err := url.Parse(record.Base)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func customMaccmsID(base string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimRight(base, "/"))))
	return customSourcePrefix + hex.EncodeToString(sum[:8])
}

func isCustomMaccmsSource(source string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(source)), customSourcePrefix)
}

func nativeEngineSnapshot() *nativeEngine {
	nativeState.Lock()
	defer nativeState.Unlock()
	return nativeState.engine
}

// customSourceRecordOf 取自定义源在注册表里的记录（非自定义源返回 false）。
func customSourceRecordOf(source string) (customMaccmsSource, bool) {
	if !isCustomMaccmsSource(source) {
		return customMaccmsSource{}, false
	}
	engine := nativeEngineSnapshot()
	if engine == nil || engine.customSources == nil {
		return customMaccmsSource{}, false
	}
	return engine.customSources.get(canonicalProviderSource(source))
}

// customSourceSite 把注册表记录转成 maccmsSite：APIBase 优先用已探测到的接口。
func customSourceSite(record customMaccmsSource) maccmsSite {
	return maccmsSite{
		Source:   record.ID,
		Name:     record.Name,
		APIBase:  firstNonEmpty(record.API, record.Base),
		SiteBase: customSourceOrigin(record.Base),
	}
}

// customSourceOrigin 取站点根地址，作为 Referer 与封面来源。
func customSourceOrigin(address string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || parsed.Host == "" {
		return strings.TrimRight(strings.TrimSpace(address), "/")
	}
	return parsed.Scheme + "://" + parsed.Host
}

// isMaccmsEndpointURL 判断地址是否已经是采集接口端点。
func isMaccmsEndpointURL(address string) bool {
	lower := strings.ToLower(address)
	return strings.Contains(lower, "/api.php") || strings.Contains(lower, "/provide/vod")
}

// normalizeCustomSourceAddress 规整用户输入：补协议、去尾斜杠、校验 host。
func normalizeCustomSourceAddress(raw string) (string, error) {
	address := strings.TrimSpace(raw)
	if address == "" {
		return "", errors.New("请输入站点地址")
	}
	if strings.HasPrefix(address, "//") {
		address = "https:" + address
	} else if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" {
		return "", errors.New("站点地址无效")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("只支持 http/https 站点")
	}
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

// customMaccmsCandidates 生成采集接口候选地址：
// 已经带 /api.php 或 /provide/vod 的只试自身，否则补标准端点。
func customMaccmsCandidates(address string) []string {
	address = strings.TrimRight(strings.TrimSpace(address), "/")
	if address == "" {
		return nil
	}
	if isMaccmsEndpointURL(address) {
		return []string{address}
	}
	return []string{
		address + "/api.php/provide/vod",
		address + "/api.php/provide/vod/at/json",
	}
}

func cleanCustomSourceName(name string) string {
	name = strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
	runes := []rune(name)
	if len(runes) > customSourceMaxNameRunes {
		name = string(runes[:customSourceMaxNameRunes])
	}
	return name
}

func customSourceDefaultName(address string) string {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" {
		return "自定义源"
	}
	return strings.TrimPrefix(parsed.Host, "www.")
}

func (engine *nativeEngine) customMaccmsSources() []customMaccmsSource {
	if engine == nil || engine.customSources == nil {
		return nil
	}
	records := engine.customSources.all()
	sort.Slice(records, func(i, j int) bool {
		left, right := strings.ToLower(records[i].Name), strings.ToLower(records[j].Name)
		if left != right {
			return left < right
		}
		return records[i].ID < records[j].ID
	})
	return records
}

// probeCustomMaccms 依次尝试候选接口，返回第一个能返回分类的端点。
func (engine *nativeEngine) probeCustomMaccms(ctx context.Context, address string) (string, []nativeCategory, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var lastErr error
	for _, candidate := range customMaccmsCandidates(address) {
		site := maccmsSite{Source: customSourcePrefix + "probe", Name: "自定义源", APIBase: candidate, SiteBase: customSourceOrigin(address)}
		page, err := engine.downloader.maccmsRequestAt(probeCtx, site, url.Values{"ac": {"list"}})
		if err != nil {
			lastErr = err
			continue
		}
		categories := maccmsCategories(page.Class, true)
		if len(categories) == 0 {
			lastErr = errors.New("接口没有返回影视分类")
			continue
		}
		return candidate, categories, nil
	}
	if lastErr == nil {
		lastErr = errors.New("没有找到可用的采集接口")
	}
	return "", nil, fmt.Errorf("无法识别该站点的苹果CMS接口：%v", lastErr)
}

func (engine *nativeEngine) addCustomMaccmsSource(name, base, api string) (customMaccmsSource, error) {
	address, err := normalizeCustomSourceAddress(base)
	if err != nil {
		return customMaccmsSource{}, err
	}
	probeTarget := address
	if strings.TrimSpace(api) != "" {
		probeTarget, err = normalizeCustomSourceAddress(api)
		if err != nil {
			return customMaccmsSource{}, err
		}
	}
	id := customMaccmsID(address)
	if _, found := engine.customSources.get(id); found {
		return customMaccmsSource{}, errors.New("该站点已在自定义源中")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	endpoint, _, err := engine.probeCustomMaccms(ctx, probeTarget)
	if err != nil {
		return customMaccmsSource{}, err
	}
	record := customMaccmsSource{
		ID: id, Name: firstNonEmpty(cleanCustomSourceName(name), customSourceDefaultName(address)),
		Base: address, API: endpoint, CreatedAt: time.Now(),
	}
	if err := engine.customSources.put(record); err != nil {
		return customMaccmsSource{}, err
	}
	return record, nil
}

func (engine *nativeEngine) updateCustomMaccmsSource(id, name, base, api string) (customMaccmsSource, error) {
	current, found := engine.customSources.get(strings.TrimSpace(id))
	if !found {
		return customMaccmsSource{}, errors.New("该自定义源不存在")
	}
	address := current.Base
	var err error
	if strings.TrimSpace(base) != "" {
		address, err = normalizeCustomSourceAddress(base)
		if err != nil {
			return customMaccmsSource{}, err
		}
	}
	probeTarget := strings.TrimRight(firstNonEmpty(strings.TrimSpace(api), address), "/")
	if endpoint := strings.TrimSpace(api); endpoint != "" {
		probeTarget, err = normalizeCustomSourceAddress(endpoint)
		if err != nil {
			return customMaccmsSource{}, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	endpoint, _, err := engine.probeCustomMaccms(ctx, probeTarget)
	if err != nil {
		return customMaccmsSource{}, err
	}
	updated := current
	if updated.ID != customMaccmsID(address) {
		// 站点换了：换 ID，并清掉旧记录，避免留下两个同名源。
		updated.ID = customMaccmsID(address)
	}
	updated.Name = firstNonEmpty(cleanCustomSourceName(name), current.Name, customSourceDefaultName(address))
	updated.Base, updated.API = address, endpoint
	engine.customSources.mu.Lock()
	if current.ID != updated.ID {
		delete(engine.customSources.sources, current.ID)
	}
	engine.customSources.sources[updated.ID] = updated
	saveErr := engine.customSources.save()
	engine.customSources.mu.Unlock()
	if saveErr != nil {
		return customMaccmsSource{}, saveErr
	}
	return updated, nil
}

func (engine *nativeEngine) removeCustomMaccmsSource(id string) error {
	if engine == nil || engine.customSources == nil {
		return errors.New("自定义源尚未就绪")
	}
	return engine.customSources.remove(strings.TrimSpace(id))
}

func (engine *nativeEngine) analyzeCustomSource(target string) (map[string]any, error) {
	address, err := normalizeCustomSourceAddress(target)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	endpoint, categories, err := engine.probeCustomMaccms(ctx, address)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"api":          endpoint,
		"name":         customSourceDefaultName(address),
		"categories":   categories,
		"categoriesOf": len(categories),
	}, nil
}

// ---- 导入导出 ----

const customSourceExportFormat = "duanju-custom-sources"

type customSourceDraft struct {
	Name string
	Base string
	API  string
}

type customSourceImportResult struct {
	Added   []customMaccmsSource `json:"added"`
	Updated []customMaccmsSource `json:"updated"`
	Skipped []string             `json:"skipped"`
	Failed  []string             `json:"failed"`
}

func (engine *nativeEngine) exportCustomMaccmsSources() (string, error) {
	records := engine.customMaccmsSources()
	type exportItem struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		API  string `json:"api,omitempty"`
	}
	items := make([]exportItem, 0, len(records))
	for _, record := range records {
		items = append(items, exportItem{Name: record.Name, URL: record.Base, API: record.API})
	}
	body, err := json.MarshalIndent(map[string]any{
		"format": customSourceExportFormat, "version": 1, "sites": items,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// importCustomMaccmsSources 解析受支持的文本并逐条落库；单条失败不影响其余。
// 导入不联网探测，接口地址在首次使用时解析。
func (engine *nativeEngine) importCustomMaccmsSources(content string) (customSourceImportResult, error) {
	result := customSourceImportResult{Added: []customMaccmsSource{}, Updated: []customMaccmsSource{}, Skipped: []string{}, Failed: []string{}}
	content = strings.TrimPrefix(strings.TrimSpace(content), "\ufeff")
	if content == "" {
		return result, errors.New("导入内容为空")
	}
	if len(content) > customSourceMaxBytes*8 {
		return result, errors.New("导入内容过大")
	}
	drafts, notes := customSourceDrafts(content)
	result.Skipped = append(result.Skipped, notes...)
	if len(drafts) == 0 {
		return result, errors.New("未能从内容中识别出任何站点")
	}
	if len(drafts) > customSourceImportLimit {
		result.Skipped = append(result.Skipped, fmt.Sprintf("超出单次导入上限 %d 个，其余已忽略", customSourceImportLimit))
		drafts = drafts[:customSourceImportLimit]
	}
	for _, draft := range drafts {
		address, err := normalizeCustomSourceAddress(draft.Base)
		if err != nil {
			result.Failed = append(result.Failed, firstNonEmpty(draft.Name, draft.Base)+"："+err.Error())
			continue
		}
		record, err := engine.importCustomMaccmsSource(draft, address)
		if err != nil {
			result.Failed = append(result.Failed, firstNonEmpty(draft.Name, address)+"："+err.Error())
			continue
		}
		if record.CreatedAt.IsZero() {
			result.Updated = append(result.Updated, record)
		} else {
			result.Added = append(result.Added, record)
		}
	}
	return result, nil
}

func (engine *nativeEngine) importCustomMaccmsSource(draft customSourceDraft, address string) (customMaccmsSource, error) {
	name := firstNonEmpty(cleanCustomSourceName(draft.Name), customSourceDefaultName(address))
	api := strings.TrimRight(strings.TrimSpace(draft.API), "/")
	if api != "" && !isMaccmsEndpointURL(api) {
		// 导入里的 api 字段可能只是站点根，交给运行期候选探测。
		api = ""
	}
	id := customMaccmsID(address)
	if existing, found := engine.customSources.get(id); found {
		updated := existing
		if name != "" {
			updated.Name = name
		}
		if api != "" {
			updated.API = api
		}
		if updated != existing {
			if err := engine.customSources.put(updated); err != nil {
				return customMaccmsSource{}, err
			}
			updated.CreatedAt = time.Time{}
		}
		return updated, nil
	}
	record := customMaccmsSource{ID: id, Name: name, Base: address, API: api, CreatedAt: time.Now()}
	if err := engine.customSources.put(record); err != nil {
		return customMaccmsSource{}, err
	}
	return record, nil
}

// customSourceDrafts 解析三种常见格式：
// 1. 本应用导出的 {"format":"duanju-custom-sources","sites":[...]}；
// 2. 数组（对象或纯网址字符串）；
// 3. TVBox / CatVod 的 {"sites":[{name,api,type}]}；
// 4. 纯文本，每行一个网址或「名称,网址」。
func customSourceDrafts(content string) ([]customSourceDraft, []string) {
	var notes []string
	trimmed := strings.TrimSpace(strings.TrimPrefix(content, "\ufeff"))
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var parsed any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			drafts := customSourceDraftsFromValue(parsed, &notes)
			if len(drafts) > 0 {
				return drafts, notes
			}
		}
	}
	var drafts []customSourceDraft
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(strings.Trim(line, ","))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		name, address := "", line
		if index := strings.IndexAny(line, ",|，"); index > 0 {
			name, address = strings.TrimSpace(line[:index]), strings.TrimSpace(line[index+1:])
		}
		if !strings.Contains(address, "://") {
			if strings.HasPrefix(address, "//") {
				address = "https:" + address
			} else {
				address = "https://" + address
			}
		}
		if parsed, err := url.Parse(address); err != nil || parsed.Host == "" {
			notes = append(notes, "无法识别的行："+line)
			continue
		}
		drafts = append(drafts, customSourceDraft{Name: name, Base: address})
	}
	return drafts, notes
}

func customSourceDraftsFromValue(value any, notes *[]string) []customSourceDraft {
	switch typed := value.(type) {
	case []any:
		var drafts []customSourceDraft
		for _, item := range typed {
			drafts = append(drafts, customSourceDraftsFromValue(item, notes)...)
		}
		return drafts
	case map[string]any:
		for _, key := range []string{"sites", "list", "urls", "data"} {
			if nested, found := typed[key]; found {
				switch nested.(type) {
				case []any, map[string]any:
					return customSourceDraftsFromValue(nested, notes)
				}
			}
		}
		address := firstStringField(typed, "url", "api", "base", "address", "homepage", "site")
		if address == "" {
			return nil
		}
		draft := customSourceDraft{
			Name: firstStringField(typed, "name", "title", "label"),
			Base: address,
			API:  firstStringField(typed, "api", "address"),
		}
		if draft.API == draft.Base {
			draft.API = ""
		}
		return []customSourceDraft{draft}
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []customSourceDraft{{Base: strings.TrimSpace(typed)}}
	}
	if notes != nil {
		*notes = append(*notes, "忽略无法识别的条目")
	}
	return nil
}

func firstStringField(entry map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := entry[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
