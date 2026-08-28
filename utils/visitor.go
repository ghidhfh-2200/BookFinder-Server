package utils

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"time"
)

// VisitorTokenBytes 访问者令牌中随机部分的字节数
const VisitorTokenBytes = 16

// visitorTokenLifetime 令牌有效期。
//
// 与 cookie 的有效期一致：报告记录永不过期，故令牌失效只影响「能否撤销自己的
// 报告」，不会让已有的报告次数丢失。
const visitorTokenLifetime = 365 * 24 * time.Hour

// 令牌解析可能遇到的错误
var (
	// ErrTokenMalformed 令牌格式不对，多半是被截断或手工编造的
	ErrTokenMalformed = errors.New("访问者令牌格式不正确")
	// ErrTokenSignature 签名不符：不是本服务签发的令牌
	ErrTokenSignature = errors.New("访问者令牌签名无效")
	// ErrTokenExpired 令牌已过期
	ErrTokenExpired = errors.New("访问者令牌已过期")
)

// visitorTokenSecret 令牌签名密钥，由 SetVisitorTokenSecret 注入
var visitorTokenSecret []byte

// SetVisitorTokenSecret 设置令牌签名密钥。
// 启动时调用一次；密钥变更会使所有已签发的令牌失效，访问者需重新领取。
func SetVisitorTokenSecret(secret []byte) {
	visitorTokenSecret = secret
}

// GenerateVisitorToken 签发访问者令牌。
//
// 令牌形如 base64(nonce||issuedAt).base64(HMAC)，服务端无需存储即可验证
// 「这是我发出去的、且尚未过期」。这是限流与封禁的身份基础：
//
// 旧实现是纯随机串，服务端无从分辨令牌的真伪，只能靠「没带就发一个新的」。
// 于是不带 cookie 的请求每次都是全新访问者、每次都拿满配额——按令牌计数的
// 限流形同不存在，这正是 API 被盗刷的主路径。改为自证令牌后，服务端可以
// 拒绝一切非自己签发的令牌，无令牌的来源则只能走按 IP 计的见习配额。
//
// 令牌仍然可被清除，清除后视为新访问者，但领取新令牌要消耗见习配额，
// 而见习配额按 IP 计——清 cookie 不再是免费的。
func GenerateVisitorToken() (string, error) {
	nonce := make([]byte, VisitorTokenBytes)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	payload := make([]byte, 0, VisitorTokenBytes+8)
	payload = append(payload, nonce...)
	payload = binary.BigEndian.AppendUint64(payload, uint64(time.Now().Unix()))

	encoded := base64.RawURLEncoding.EncodeToString(payload)

	return encoded + "." + base64.RawURLEncoding.EncodeToString(signPayload(payload)), nil
}

// ParseVisitorToken 校验令牌并返回其签发时间。
//
// 校验失败一律不下发新令牌：伪造令牌的请求应当落到见习配额上，
// 若此时补发一个正式令牌，攻击者只要每次发一个乱码就能白拿配额。
func ParseVisitorToken(token string) (time.Time, error) {
	encodedPayload, encodedSign, found := cutLast(token, '.')
	if !found {
		return time.Time{}, ErrTokenMalformed
	}

	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil || len(payload) != VisitorTokenBytes+8 {
		return time.Time{}, ErrTokenMalformed
	}

	sign, err := base64.RawURLEncoding.DecodeString(encodedSign)
	if err != nil {
		return time.Time{}, ErrTokenMalformed
	}

	// 恒定时间比较，避免按字节逐位试探签名
	if !hmac.Equal(sign, signPayload(payload)) {
		return time.Time{}, ErrTokenSignature
	}

	issuedAt := time.Unix(int64(binary.BigEndian.Uint64(payload[VisitorTokenBytes:])), 0)

	// 签发时间明显在未来说明时钟不一致或令牌被改造，按无效处理
	if time.Until(issuedAt) > time.Hour {
		return time.Time{}, ErrTokenMalformed
	}
	if time.Since(issuedAt) > visitorTokenLifetime {
		return issuedAt, ErrTokenExpired
	}

	return issuedAt, nil
}

// signPayload 计算令牌载荷的签名
func signPayload(payload []byte) []byte {
	mac := hmac.New(sha256.New, visitorTokenSecret)
	mac.Write(payload)
	return mac.Sum(nil)
}

// cutLast 按最后一个分隔符切分，用于分离载荷与签名
func cutLast(s string, sep byte) (before, after string, found bool) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == sep {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// HashVisitorToken 计算令牌哈希。
// 库中只存哈希，即便数据泄露也无法反推出可用的令牌。
func HashVisitorToken(token string) string {
	return sha256Hex(token)
}

// HashVisitorSignal 计算设备特征信号的哈希。
// 原始信号带有设备特征，只存哈希；长度也因此固定，便于建索引。
func HashVisitorSignal(signal string) string {
	if signal == "" {
		return ""
	}
	return sha256Hex(signal)
}

// HashDeviceID 计算安卓设备标识的哈希。
// 与令牌哈希同长，便于共用封禁标识表的字段。
func HashDeviceID(deviceID string) string {
	if deviceID == "" {
		return ""
	}
	return sha256Hex(deviceID)
}

// sha256Hex 取 SHA-256 的十六进制表示
func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
