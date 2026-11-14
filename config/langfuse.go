package config

import (
	"time"

	"github.com/cloudwego/eino-ext/callbacks/langfuse"
)

// LangfuseConfig owns the YAML schema instead of relying on the SDK's untagged
// Go fields. Duration values include units, e.g. timeout: 30s.
type LangfuseConfig struct {
	Host             string        `yaml:"host"`
	PublicKey        string        `yaml:"public_key"`
	SecretKey        string        `yaml:"secret_key"`
	Threads          int           `yaml:"threads,omitempty"`
	Timeout          time.Duration `yaml:"timeout,omitempty"`
	MaxTaskQueueSize int           `yaml:"max_task_queue_size,omitempty"`
	FlushAt          int           `yaml:"flush_at,omitempty"`
	FlushInterval    time.Duration `yaml:"flush_interval,omitempty"`
	SampleRate       float64       `yaml:"sample_rate,omitempty"`
	LogMessage       string        `yaml:"log_message,omitempty"`
	MaxRetry         uint64        `yaml:"max_retry,omitempty"`
	Name             string        `yaml:"name,omitempty"`
	UserID           string        `yaml:"user_id,omitempty"`
	SessionID        string        `yaml:"session_id,omitempty"`
	Release          string        `yaml:"release,omitempty"`
	Tags             []string      `yaml:"tags,omitempty"`
	Public           bool          `yaml:"public,omitempty"`
}

// SDKConfig converts the application schema to the upstream callback options.
func (c *LangfuseConfig) SDKConfig() *langfuse.Config {
	return &langfuse.Config{
		Host:             c.Host,
		PublicKey:        c.PublicKey,
		SecretKey:        c.SecretKey,
		Threads:          c.Threads,
		Timeout:          c.Timeout,
		MaxTaskQueueSize: c.MaxTaskQueueSize,
		FlushAt:          c.FlushAt,
		FlushInterval:    c.FlushInterval,
		SampleRate:       c.SampleRate,
		LogMessage:       c.LogMessage,
		MaxRetry:         c.MaxRetry,
		Name:             c.Name,
		UserID:           c.UserID,
		SessionID:        c.SessionID,
		Release:          c.Release,
		Tags:             append([]string(nil), c.Tags...),
		Public:           c.Public,
	}
}
