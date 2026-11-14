package config

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestLangfuseSnakeCaseFieldsReachSDK(t *testing.T) {
	decoder := yaml.NewDecoder(strings.NewReader(`host: https://example.invalid
public_key: pk-test
secret_key: sk-test
max_task_queue_size: 1000
flush_at: 20
flush_interval: 500ms
timeout: 30s
sample_rate: 0.5
max_retry: 5
log_message: trace
user_id: user-1
session_id: session-1
tags: [test]
public: true
`))
	decoder.KnownFields(true)
	var cfg LangfuseConfig
	if err := decoder.Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	sdk := cfg.SDKConfig()
	if sdk.PublicKey != "pk-test" || sdk.SecretKey != "sk-test" || sdk.Timeout != 30*time.Second || sdk.FlushInterval != 500*time.Millisecond || sdk.UserID != "user-1" || sdk.SessionID != "session-1" || sdk.MaxTaskQueueSize != 1000 || sdk.MaxRetry != 5 || sdk.SampleRate != 0.5 || !sdk.Public {
		t.Fatal("snake_case configuration was not forwarded to the SDK")
	}
	sdk.Tags[0] = "changed"
	if cfg.Tags[0] != "test" {
		t.Fatal("SDK configuration shares mutable tags with YAML configuration")
	}
}
