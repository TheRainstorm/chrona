package chrona

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const ExpiryProperty = "chrona:expires"
const SnapshotPrefix = "chrona_"

type Keep struct {
	QuarterHourly int `json:"quarter_hourly" yaml:"quarter_hourly"`
	Hourly        int `json:"hourly" yaml:"hourly"`
	Daily         int `json:"daily" yaml:"daily"`
	Weekly        int `json:"weekly" yaml:"weekly"`
	Monthly       int `json:"monthly" yaml:"monthly"`
}

func (k Keep) Values() []int { return []int{k.QuarterHourly, k.Hourly, k.Daily, k.Weekly, k.Monthly} }

type Policy struct {
	ID       string   `json:"id" yaml:"id"`
	Dataset  string   `json:"dataset,omitempty" yaml:"dataset,omitempty"` // v1 input / resolved scheduler binding
	Datasets []string `json:"datasets" yaml:"datasets"`
	Label    string   `json:"label" yaml:"label"`
	StartAt  string   `json:"start_at" yaml:"start_at"`
	Enabled  bool     `json:"enabled" yaml:"enabled"`
	Keep     Keep     `json:"keep" yaml:"keep"`
}

type BackupDataset struct {
	Source      string `json:"source" yaml:"source"`
	Destination string `json:"destination" yaml:"destination"`
	Disabled    bool   `json:"disabled,omitempty" yaml:"disabled,omitempty"`
}

// A shared backup policy contains time settings only; destinations belong to jobs.
type BackupPolicy struct {
	ID           string `json:"id" yaml:"id"`
	Name         string `json:"name" yaml:"name"`
	Cadence      string `json:"cadence" yaml:"cadence"`
	Weekdays     []int  `json:"weekdays" yaml:"weekdays"`
	MonthDay     int    `json:"month_day" yaml:"month_day"`
	At           string `json:"at" yaml:"at"`
	GraceMinutes int    `json:"grace_minutes,omitempty" yaml:"grace_minutes,omitempty"`
}

type Backup struct {
	ID          string          `json:"id" yaml:"id"`
	Name        string          `json:"name,omitempty" yaml:"name,omitempty"`
	Policy      string          `json:"policy,omitempty" yaml:"policy,omitempty"`
	PolicyName  string          `json:"policy_name,omitempty" yaml:"-"`
	Source      string          `json:"source,omitempty" yaml:"source,omitempty"` // v1 input only
	Datasets    []BackupDataset `json:"datasets" yaml:"datasets"`
	Host        string          `json:"host" yaml:"host"`
	Port        int             `json:"port" yaml:"port"`
	Destination string          `json:"destination,omitempty" yaml:"destination,omitempty"` // v1 input only
	Cadence     string          `json:"cadence,omitempty" yaml:"cadence,omitempty"`
	Weekdays    []int           `json:"weekdays,omitempty" yaml:"weekdays,omitempty"`
	MonthDay    int             `json:"month_day,omitempty" yaml:"month_day,omitempty"`
	At          string          `json:"at,omitempty" yaml:"at,omitempty"`
	Enabled     bool            `json:"enabled" yaml:"enabled"`
	// Deprecated input fields: NormalizeConfig discards historical retention counts.
	KeepWeekly   int  `json:"keep_weekly,omitempty" yaml:"keep_weekly,omitempty"`
	KeepMonthly  int  `json:"keep_monthly,omitempty" yaml:"keep_monthly,omitempty"`
	Private      bool `json:"private,omitempty" yaml:"private,omitempty"`
	GraceMinutes int  `json:"grace_minutes,omitempty" yaml:"grace_minutes,omitempty"`
}

type Config struct {
	Version        int             `json:"version" yaml:"version"`
	Timezone       string          `json:"timezone" yaml:"timezone"`
	JitterSeconds  int             `json:"jitter_seconds" yaml:"jitter_seconds"`
	Policies       []Policy        `json:"policies" yaml:"policies"`
	BackupPolicies []BackupPolicy  `json:"backup_policies" yaml:"backup_policies"`
	Backups        []Backup        `json:"backups" yaml:"backups"`
	Datasets       []DatasetConfig `json:"datasets" yaml:"datasets"`
}

var datasetPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.:-]*(/[A-Za-z0-9_.:%-]+)*$`)
var idPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)
var hostPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.@-]{0,180}$`)

func DecodeConfig(data []byte) (Config, error) {
	var c Config
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&c); err != nil {
		return c, fmt.Errorf("配置格式错误：%w", err)
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return c, errors.New("配置只能包含一个 YAML 文档")
	}
	c = NormalizeConfig(c)
	return c, ValidateConfig(c)
}

// A policy is independent from its dataset assignments. v1 files are migrated
// without changing any retention or enabled setting; identical rules are shared.
func normalizeLegacyConfig(c Config) Config {
	legacy := c.Version == 1
	result := c
	result.Policies = []Policy{}
	for _, p := range c.Policies {
		if legacy && strings.TrimSpace(p.Label) == "" {
			p.Label = p.ID
		}
		p.Datasets = append([]string{}, p.Datasets...)
		if p.Dataset != "" {
			p.Datasets = append(p.Datasets, p.Dataset)
			p.Dataset = ""
		}
		merged := false
		if legacy {
			for i, existing := range result.Policies {
				if existing.Keep == p.Keep && existing.StartAt == p.StartAt && existing.Enabled == p.Enabled {
					result.Policies[i].Datasets = append(result.Policies[i].Datasets, p.Datasets...)
					if existing.Label != p.Label && len(existing.Label+" / "+p.Label) <= 160 {
						result.Policies[i].Label = existing.Label + " / " + p.Label
					}
					merged = true
					break
				}
			}
		}
		if !merged {
			result.Policies = append(result.Policies, p)
		}
	}
	result.Backups = append([]Backup{}, c.Backups...)
	for i := range result.Backups {
		b := &result.Backups[i]
		b.Datasets = append([]BackupDataset{}, b.Datasets...)
		if b.Source != "" {
			b.Datasets = append(b.Datasets, BackupDataset{Source: b.Source, Destination: b.Destination})
			b.Source = ""
			b.Destination = ""
		}
	}
	if legacy {
		result.Version = 2
	}
	return result
}

func ResolvePolicies(policies []Policy) []Policy {
	result := []Policy{}
	for _, p := range policies {
		if p.Dataset != "" {
			result = append(result, p)
			continue
		}
		for _, dataset := range p.Datasets {
			binding := p
			binding.Dataset = dataset
			result = append(result, binding)
		}
	}
	return result
}

func backupSources(b Backup) string {
	names := []string{}
	for _, d := range b.Datasets {
		names = append(names, d.Source)
	}
	return strings.Join(names, ", ")
}

func ValidateConfig(c Config) error {
	if c.Version != 3 {
		return errors.New("version 必须为 3")
	}
	if c.Timezone == "" || c.Timezone == "Local" {
		return errors.New("请选择明确的 IANA 时区，例如 Asia/Shanghai")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return errors.New("请选择有效的 IANA 时区")
	}
	if c.JitterSeconds < 0 || c.JitterSeconds > 300 {
		return errors.New("随机偏移必须在 0～300 秒之间")
	}
	if len(c.Policies) > 200 || len(c.BackupPolicies) > 100 || len(c.Backups) > 100 {
		return errors.New("Demo 最多支持 200 个快照策略和 100 个备份任务")
	}
	ids, datasets := map[string]bool{}, map[string]bool{}
	for _, p := range c.Policies {
		if ids[p.ID] {
			return errors.New("策略 ID 不能重复")
		}
		ids[p.ID] = true
		if len(p.Datasets) > 0 || p.Dataset != "" {
			return errors.New("公共策略中不再保存数据集，请在 datasets 中引用策略")
		}
		if err := ValidatePolicy(p); err != nil {
			return fmt.Errorf("%s：%w", p.Label, err)
		}
	}
	if len(c.Datasets) > 2000 {
		return errors.New("最多管理 2000 个数据集")
	}
	for _, d := range c.Datasets {
		if !datasetPattern.MatchString(d.Name) {
			return errors.New("dataset 名称无效")
		}
		if datasets[d.Name] {
			return fmt.Errorf("%s 重复添加", d.Name)
		}
		datasets[d.Name] = true
		if err := validateDatasetConfig(c, d); err != nil {
			return fmt.Errorf("%s：%w", d.Name, err)
		}
	}
	ids = map[string]bool{}
	for _, p := range c.BackupPolicies {
		if ids[p.ID] {
			return errors.New("备份时间策略 ID 不能重复")
		}
		ids[p.ID] = true
		if err := ValidateBackupPolicy(p); err != nil {
			return err
		}
	}
	ids = map[string]bool{}
	for _, b := range c.Backups {
		if ids[b.ID] {
			return errors.New("备份 ID 不能重复")
		}
		ids[b.ID] = true
		if b.Policy != "" {
			if b.Cadence != "" || len(b.Weekdays) > 0 || b.MonthDay != 0 || b.At != "" || b.GraceMinutes != 0 {
				return errors.New("引用公共备份时间策略时不能同时设置独立时间")
			}
			found := false
			for _, p := range c.BackupPolicies {
				found = found || p.ID == b.Policy
			}
			if !found {
				return fmt.Errorf("备份 %s 引用的时间策略 %s 不存在", b.ID, b.Policy)
			}
		}
		if err := ValidateBackup(c.ResolveBackup(b)); err != nil {
			return err
		}
		for _, binding := range b.Datasets {
			if !c.IsManagedDataset(binding.Source) {
				return fmt.Errorf("备份源 %s 尚未添加管理", binding.Source)
			}
		}
	}
	return nil
}

func ValidatePolicy(p Policy) error {
	if !idPattern.MatchString(p.ID) {
		return errors.New("策略 ID 只能使用字母、数字、下划线和短横线")
	}
	for _, dataset := range p.Datasets {
		if !datasetPattern.MatchString(dataset) {
			return errors.New("dataset 名称无效")
		}
	}
	if p.Dataset != "" && !datasetPattern.MatchString(p.Dataset) {
		return errors.New("dataset 名称无效")
	}
	if len(p.Label) > 160 || strings.TrimSpace(p.Label) == "" {
		return errors.New("请填写策略名称（最长 160 字节）")
	}
	anchor, err := time.Parse(time.RFC3339, p.StartAt)
	if err != nil || anchor.Year() < 2000 || anchor.Year() > 2100 {
		return errors.New("起始时间应为 2000～2100 年之间的 RFC3339 时间")
	}
	counts := p.Keep.Values()
	previous, previousName, active := int64(0), "", false
	for i, count := range counts {
		if count < 0 || count > 10000 {
			return errors.New("每档保留数量必须是 0～10000 的整数")
		}
		if count == 0 {
			continue
		}
		active = true
		span := int64(Tiers[i].Seconds) * int64(count)
		if i == 4 {
			span = minimumMonthSpan(anchor.Day(), count)
		}
		if previous > span {
			return fmt.Errorf("%s档保留跨度超过%s档，请增加%s数量或减少%s数量", previousName, Tiers[i].Label, Tiers[i].Label, previousName)
		}
		previous, previousName = span, Tiers[i].Label
	}
	if !active {
		return errors.New("至少启用一档快照；暂停任务请使用启用开关")
	}
	return nil
}

func ValidateBackup(b Backup) error {
	if !idPattern.MatchString(b.ID) {
		return errors.New("备份 ID 无效")
	}
	if len(b.Name) > 160 {
		return errors.New("备份名称最长 160 字节")
	}
	if b.Private && len(b.Datasets) != 1 {
		return errors.New("独立备份策略必须且只能属于一个数据集")
	}
	sources, targets := map[string]bool{}, map[string]bool{}
	for _, d := range b.Datasets {
		if !datasetPattern.MatchString(d.Source) || !datasetPattern.MatchString(d.Destination) {
			return errors.New("源或目标 dataset 名称无效")
		}
		if sources[d.Source] || targets[d.Destination] {
			return errors.New("同一备份策略中的源或目标数据集不能重复")
		}
		sources[d.Source] = true
		targets[d.Destination] = true
	}
	if !hostPattern.MatchString(b.Host) || b.Port < 1 || b.Port > 65535 {
		return errors.New("请填写有效的 SSH 主机和端口")
	}
	return validateBackupTime(b)
}

func ValidateBackupPolicy(p BackupPolicy) error {
	if !idPattern.MatchString(p.ID) {
		return errors.New("备份时间策略 ID 无效")
	}
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 160 {
		return errors.New("请填写备份时间策略名称（最长 160 字节）")
	}
	return validateBackupTime(p.apply(Backup{}))
}

func validateBackupTime(b Backup) error {
	if b.GraceMinutes < 0 || b.GraceMinutes > 10080 {
		return errors.New("备份缓冲时间必须在 1～10080 分钟之间；省略时默认 60 分钟")
	}
	if _, err := time.Parse("15:04", b.At); err != nil {
		return errors.New("备份时间格式应为 HH:MM")
	}
	if b.Cadence != "weekly" && b.Cadence != "monthly" {
		return errors.New("备份频率只能是每周或每月")
	}
	if b.Cadence == "weekly" && len(b.Weekdays) == 0 {
		return errors.New("至少选择一个备份星期")
	}
	for _, day := range b.Weekdays {
		if day < 0 || day > 6 {
			return errors.New("星期无效")
		}
	}
	if b.MonthDay < 1 || b.MonthDay > 31 {
		return errors.New("每月日期必须在 1～31 日之间")
	}
	return nil
}

// Check all months in the Gregorian 400-year cycle. A calendar month is not 30 days.
func minimumMonthSpan(day, count int) int64 {
	min := int64(1<<63 - 1)
	for year := 2000; year < 2400; year++ {
		for month := time.January; month <= time.December; month++ {
			last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
			d := day
			if d > last {
				d = last
			}
			start := time.Date(year, month, d, 0, 0, 0, 0, time.UTC)
			end := addMonthsClamped(start, count)
			span := int64(end.Sub(start).Seconds())
			// time.Duration overflows for spans above ~292 years.
			if count > 3000 {
				span = end.Unix() - start.Unix()
			}
			if span < min {
				min = span
			}
		}
	}
	return min
}
