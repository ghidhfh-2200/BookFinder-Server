package sysconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadPreservesExplicitFalse 文件里显式写 false 必须压过默认的 true。
//
// 这是「以默认值为底再反序列化」引入的风险：布尔项默认为 true，若合并有误，
// 用户关掉的告警会在重启后自己打开。
func TestLoadPreservesExplicitFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	raw := `{"maintenance":{"enabled":true,"daily_at":"03:30",
"operation_log_retention_days":180,"app_log_retention_days":30},
"notify":{"auto_ban":false,"network_anomaly":false,"appeal":false},
"pagination":{"default_size":20,"max_size":100},
"server":{"max_request_body_bytes":65536,"max_concurrent_requests":256,
"read_header_timeout_seconds":10,"read_timeout_seconds":30,
"write_timeout_seconds":60,"idle_timeout_seconds":90}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Load(path); err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	n := Get().Notify
	if n.AutoBan || n.NetworkAnomaly || n.Appeal {
		t.Errorf("显式的 false 被默认值覆盖了: %+v", n)
	}
	// 而缺失的 email 段应拿到默认端口，不是 0
	if Get().Notify.Email.Port == 0 {
		t.Error("缺失的 email 段未拿到默认端口")
	}
}
