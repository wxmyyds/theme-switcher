package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const filePerm = 0644

const (
	minHour = 0
	maxHour = 23

	defaultLightTime = 6
	defaultDarkTime  = 18
)

var (
	// ErrParseFailed 表示配置文件存在但内容无法解析。
	// 调用方必须据此跳过任何"把配置写回系统"的操作（例如重建计划任务），
	// 否则会用默认值覆盖用户的自定义设置。
	ErrParseFailed = errors.New("配置文件解析失败")

	// ErrAdjusted 表示配置可用，但其中若干字段非法并已被自动纠正。
	// 这是非致命错误，纠正后的配置可以直接使用。
	ErrAdjusted = errors.New("配置存在非法字段并已自动纠正")
)

type Config struct {
	LightModeWhiteText bool `json:"light_mode_white_text"`
	DarkModeWhiteText  bool `json:"dark_mode_white_text"`
	LightTimeStart     int  `json:"light_time_start"`
	DarkTimeStart      int  `json:"dark_time_start"`
	EnableLogging      bool `json:"enable_logging"`
}

// DefaultConfig 返回一份默认配置的副本。
// 刻意不使用包级变量，避免调用方意外修改全局默认值。
func DefaultConfig() Config {
	return Config{
		LightModeWhiteText: false,
		DarkModeWhiteText:  true,
		LightTimeStart:     defaultLightTime,
		DarkTimeStart:      defaultDarkTime,
		EnableLogging:      true,
	}
}

// Validate 就地纠正非法字段，并返回被纠正项的说明列表。
func (c *Config) Validate() (notes []string) {
	if c.LightTimeStart < minHour || c.LightTimeStart > maxHour {
		notes = append(notes, fmt.Sprintf("light_time_start=%d 不在 %d-%d 范围内，已重置为 %d",
			c.LightTimeStart, minHour, maxHour, defaultLightTime))
		c.LightTimeStart = defaultLightTime
	}
	if c.DarkTimeStart < minHour || c.DarkTimeStart > maxHour {
		notes = append(notes, fmt.Sprintf("dark_time_start=%d 不在 %d-%d 范围内，已重置为 %d",
			c.DarkTimeStart, minHour, maxHour, defaultDarkTime))
		c.DarkTimeStart = defaultDarkTime
	}
	// 两个起始时间相等是无效配置：此时 theme.ShouldUseLightMode 会退化成
	// "恒为浅色"，深色模式永远不会生效。这里把深色起始时间推后 12 小时消除歧义。
	if c.LightTimeStart == c.DarkTimeStart {
		c.DarkTimeStart = (c.LightTimeStart + 12) % 24
		notes = append(notes, fmt.Sprintf("浅色与深色起始时间相同，深色起始时间已调整为 %d", c.DarkTimeStart))
	}
	return notes
}

// rawConfig 用于容错解析。时间字段先用 json.RawMessage 接收，
// 这样 6、"6"、6.0 三种写法都能接受，且单个字段出错不会丢弃整份配置。
type rawConfig struct {
	LightModeWhiteText *bool           `json:"light_mode_white_text"`
	DarkModeWhiteText  *bool           `json:"dark_mode_white_text"`
	LightTimeStart     json.RawMessage `json:"light_time_start"`
	DarkTimeStart      json.RawMessage `json:"dark_time_start"`
	EnableLogging      *bool           `json:"enable_logging"`
}

// applyTo 把解析结果合并进 cfg（缺省字段保持默认值），返回纠正说明。
func (r rawConfig) applyTo(cfg *Config) (notes []string) {
	if r.LightModeWhiteText != nil {
		cfg.LightModeWhiteText = *r.LightModeWhiteText
	}
	if r.DarkModeWhiteText != nil {
		cfg.DarkModeWhiteText = *r.DarkModeWhiteText
	}
	if r.EnableLogging != nil {
		cfg.EnableLogging = *r.EnableLogging
	}

	if v, ok := parseHour(r.LightTimeStart); ok {
		cfg.LightTimeStart = v
	} else if len(r.LightTimeStart) > 0 {
		notes = append(notes, fmt.Sprintf("light_time_start=%s 不是有效小时数，已使用默认值 %d",
			string(r.LightTimeStart), cfg.LightTimeStart))
	}
	if v, ok := parseHour(r.DarkTimeStart); ok {
		cfg.DarkTimeStart = v
	} else if len(r.DarkTimeStart) > 0 {
		notes = append(notes, fmt.Sprintf("dark_time_start=%s 不是有效小时数，已使用默认值 %d",
			string(r.DarkTimeStart), cfg.DarkTimeStart))
	}

	return append(notes, cfg.Validate()...)
}

// parseHour 把 JSON 值转成小时数。空值（字段缺失）返回 ok=false。
func parseHour(raw json.RawMessage) (int, bool) {
	s := strings.TrimSpace(string(raw))
	if len(s) == 0 {
		return 0, false
	}
	s = strings.Trim(s, `"`) // 兼容 "6" 这种字符串写法
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return int(f), true
}

// Load 读取配置文件。返回的 error 可用 errors.Is 判定：
//   - ErrParseFailed：内容无法解析，cfg 中是默认值，**不应**写回系统；
//   - ErrAdjusted：配置可用但被自动纠正；
//   - 其他：文件读取或创建失败。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		cfg := DefaultConfig()
		if os.IsNotExist(err) {
			return &cfg, Save(path, &cfg)
		}
		return &cfg, err
	}

	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		cfg := DefaultConfig()
		return &cfg, fmt.Errorf("%w: %v", ErrParseFailed, err)
	}

	cfg := DefaultConfig()
	if notes := raw.applyTo(&cfg); len(notes) > 0 {
		return &cfg, fmt.Errorf("%w: %s", ErrAdjusted, strings.Join(notes, "; "))
	}
	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, filePerm)
}
