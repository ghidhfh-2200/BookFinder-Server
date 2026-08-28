package utils

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"
)

// setSecret 设置测试用的签名密钥
func setSecret(t *testing.T) {
	t.Helper()
	SetVisitorTokenSecret([]byte("test-secret-do-not-use-in-production"))
}

// TestGenerateAndParse 自己签发的令牌应能通过校验
func TestGenerateAndParse(t *testing.T) {
	setSecret(t)

	token, err := GenerateVisitorToken()
	if err != nil {
		t.Fatalf("签发令牌失败: %v", err)
	}

	issuedAt, err := ParseVisitorToken(token)
	if err != nil {
		t.Fatalf("校验自己签发的令牌应通过，实际报错: %v", err)
	}

	if time.Since(issuedAt) > time.Minute {
		t.Errorf("签发时间应为刚刚，实际为 %v", issuedAt)
	}
}

// TestTokensAreUnique 每次签发应得到不同令牌，否则所有访问者会共享同一份配额
func TestTokensAreUnique(t *testing.T) {
	setSecret(t)

	seen := make(map[string]struct{}, 100)
	for range 100 {
		token, err := GenerateVisitorToken()
		if err != nil {
			t.Fatalf("签发令牌失败: %v", err)
		}
		if _, dup := seen[token]; dup {
			t.Fatal("签发出了重复的令牌")
		}
		seen[token] = struct{}{}
	}
}

// TestParseRejectsForged 伪造的令牌应被拒绝。
//
// 这是限流的根基：旧实现是纯随机串，服务端无从分辨真伪，只能「没带就发一个新的」，
// 于是不带 cookie 的请求每次都拿满配额。
func TestParseRejectsForged(t *testing.T) {
	setSecret(t)

	valid, err := GenerateVisitorToken()
	if err != nil {
		t.Fatalf("签发令牌失败: %v", err)
	}
	payload, _, _ := strings.Cut(valid, ".")

	cases := map[string]string{
		"空串":          "",
		"没有分隔符":       "abcdef",
		"只有载荷":        payload + ".",
		"签名是乱码":       payload + ".bm90LWEtc2lnbmF0dXJl",
		"载荷非法 base64": "!!!." + strings.Split(valid, ".")[1],
		"载荷长度不对":      base64.RawURLEncoding.EncodeToString([]byte("short")) + ".c2ln",
		"纯随机串":        "8f14e45fceea167a5a36dedd4bea2543",
	}

	for name, token := range cases {
		if _, err := ParseVisitorToken(token); err == nil {
			t.Errorf("令牌「%s」应被拒绝，实际通过了", name)
		}
	}
}

// TestParseRejectsTamperedPayload 改动载荷会使签名失配
func TestParseRejectsTamperedPayload(t *testing.T) {
	setSecret(t)

	token, err := GenerateVisitorToken()
	if err != nil {
		t.Fatalf("签发令牌失败: %v", err)
	}

	encodedPayload, encodedSign, _ := strings.Cut(token, ".")
	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		t.Fatalf("解码载荷失败: %v", err)
	}

	// 改一个字节
	payload[0] ^= 0xFF
	tampered := base64.RawURLEncoding.EncodeToString(payload) + "." + encodedSign

	if _, err := ParseVisitorToken(tampered); !errors.Is(err, ErrTokenSignature) {
		t.Errorf("篡改载荷应报签名无效，实际为: %v", err)
	}
}

// TestParseRejectsOtherSecret 换了密钥签发的令牌不该被认，
// 否则密钥泄露后攻击者可以自行签发无限身份
func TestParseRejectsOtherSecret(t *testing.T) {
	SetVisitorTokenSecret([]byte("secret-one"))

	token, err := GenerateVisitorToken()
	if err != nil {
		t.Fatalf("签发令牌失败: %v", err)
	}

	SetVisitorTokenSecret([]byte("secret-two"))

	if _, err := ParseVisitorToken(token); !errors.Is(err, ErrTokenSignature) {
		t.Errorf("换密钥后应报签名无效，实际为: %v", err)
	}
}

// TestParseRejectsExpired 过期令牌应被识别
func TestParseRejectsExpired(t *testing.T) {
	setSecret(t)

	// 手工构造一个一年前签发的令牌
	stale := time.Now().Add(-visitorTokenLifetime - time.Hour)
	token := forgeToken(t, stale)

	if _, err := ParseVisitorToken(token); !errors.Is(err, ErrTokenExpired) {
		t.Errorf("过期令牌应报已过期，实际为: %v", err)
	}
}

// TestParseAcceptsNearExpiry 尚未过期的旧令牌应仍然可用
func TestParseAcceptsNearExpiry(t *testing.T) {
	setSecret(t)

	token := forgeToken(t, time.Now().Add(-visitorTokenLifetime+time.Hour))

	if _, err := ParseVisitorToken(token); err != nil {
		t.Errorf("将到期但仍有效的令牌应通过，实际报错: %v", err)
	}
}

// TestParseRejectsFutureIssuedAt 签发时间在未来说明令牌被改造过
func TestParseRejectsFutureIssuedAt(t *testing.T) {
	setSecret(t)

	token := forgeToken(t, time.Now().Add(48*time.Hour))

	if _, err := ParseVisitorToken(token); err == nil {
		t.Error("签发时间在未来的令牌应被拒绝")
	}
}

// forgeToken 用当前密钥签发一个指定签发时间的令牌，供过期相关用例使用
func forgeToken(t *testing.T, issuedAt time.Time) string {
	t.Helper()

	payload := make([]byte, 0, VisitorTokenBytes+8)
	payload = append(payload, make([]byte, VisitorTokenBytes)...)
	payload = binary.BigEndian.AppendUint64(payload, uint64(issuedAt.Unix()))

	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(signPayload(payload))
}

// TestHashVisitorToken 哈希应稳定且不可反推
func TestHashVisitorToken(t *testing.T) {
	first := HashVisitorToken("some-token")
	second := HashVisitorToken("some-token")

	if first != second {
		t.Error("同一令牌的哈希应稳定，否则同一人会被当成多个访问者")
	}
	if first == "some-token" {
		t.Error("哈希不应等于原文")
	}
	if len(first) != 64 {
		t.Errorf("SHA-256 十六进制应为 64 字符，实际 %d", len(first))
	}
	if HashVisitorToken("other-token") == first {
		t.Error("不同令牌不应得到相同哈希")
	}
}

// TestHashEmptyInputs 空输入应返回空串，避免所有未上报者共享同一个哈希值
func TestHashEmptyInputs(t *testing.T) {
	if HashVisitorSignal("") != "" {
		t.Error("空信号应返回空串")
	}
	if HashDeviceID("") != "" {
		t.Error("空设备标识应返回空串")
	}
}

// TestHashDeviceID 设备标识哈希与令牌哈希同长，便于共用封禁标识表的字段
func TestHashDeviceID(t *testing.T) {
	hashed := HashDeviceID("android-id-plus-uuid")
	if len(hashed) != 64 {
		t.Errorf("设备标识哈希应为 64 字符，实际 %d", len(hashed))
	}
	if hashed == "android-id-plus-uuid" {
		t.Error("哈希不应等于原文")
	}
}
