package middlewares

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// signedRequest 构造一个签名正确的请求，供校验逻辑的用例复用
func signedRequest(t *testing.T, secret, device, nonce string) *gin.Context {
	t.Helper()

	gin.SetMode(gin.TestMode)

	const method = http.MethodGet
	const path = "/api/libraries"

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	bodyHash := sha256.Sum256(nil)
	mac := hmac.New(sha256.New, []byte(secret))
	for _, part := range []string{
		method, path, timestamp, nonce, device, hex.EncodeToString(bodyHash[:]),
	} {
		mac.Write([]byte(part))
		mac.Write([]byte("\n"))
	}
	sign := hex.EncodeToString(mac.Sum(nil))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(method, path, nil)
	c.Request.Header.Set(DeviceHeaderName, device)
	c.Request.Header.Set(TimestampHeaderName, timestamp)
	c.Request.Header.Set(NonceHeaderName, nonce)
	c.Request.Header.Set(SignHeaderName, sign)

	return c
}

// TestAntiReplayOutageDegradesInsteadOfRejecting 防重放设施不可用时应降级放行，
// 而不是拒绝请求。
//
// 这一处的取舍：拒绝的话，打掉 Redis 就能让整个安卓端不可用——把一个可用性问题
// 换成了更严重的可用性问题。降级的代价是这段时间内不采信设备标识、重放窗口回到
// signSkew 那么大，两者都好过整端不可用。
//
// 用例在 Redis 未连接的前提下跑（测试进程不初始化 Redis 连接），
// 这正是 consumeNonce 返回 errAntiReplayUnavailable 的情形。
func TestAntiReplayOutageDegradesInsteadOfRejecting(t *testing.T) {
	const secret = "test-secret"

	SetClientSignSecret([]byte(secret))
	defer SetClientSignSecret(nil)

	c := signedRequest(t, secret, "device-abc", "nonce-0123456789abcdef")

	err := verifySignature(c, "device-abc", c.GetHeader(SignHeaderName), nil)
	if err == nil {
		t.Fatal("Redis 未连接时 consumeNonce 应报错，测试前提不成立")
	}

	// 中间件据此区分「我方故障」与「伪造证据」：前者降级，后者拒绝
	if !isAntiReplayUnavailable(err) {
		t.Errorf("防重放不可用应可被识别，实际错误为: %v", err)
	}
}

// TestBadSignatureIsNotTreatedAsOutage 签名不符是伪造的证据，绝不能走降级路径。
//
// 这是降级方案的边界：若把签名错误也当成设施故障，任何人构造一个乱签名
// 就能让请求照常通过，验签这一层等于不存在。
func TestBadSignatureIsNotTreatedAsOutage(t *testing.T) {
	const secret = "test-secret"

	SetClientSignSecret([]byte(secret))
	defer SetClientSignSecret(nil)

	c := signedRequest(t, secret, "device-abc", "nonce-0123456789abcdef")

	err := verifySignature(c, "device-abc", "这不是正确的签名", nil)
	if err == nil {
		t.Fatal("签名不符应报错")
	}
	if isAntiReplayUnavailable(err) {
		t.Error("签名不符不应被当作防重放故障——那样伪造签名即可绕过验签")
	}
}

// TestExpiredTimestampIsNotTreatedAsOutage 时间戳越界同样是拒绝的理由，不是故障
func TestExpiredTimestampIsNotTreatedAsOutage(t *testing.T) {
	const secret = "test-secret"

	SetClientSignSecret([]byte(secret))
	defer SetClientSignSecret(nil)

	c := signedRequest(t, secret, "device-abc", "nonce-0123456789abcdef")
	// 把时间戳改到远超允许偏差之外
	c.Request.Header.Set(TimestampHeaderName,
		strconv.FormatInt(time.Now().Add(-2*signSkew).Unix(), 10))

	err := verifySignature(c, "device-abc", c.GetHeader(SignHeaderName), nil)
	if err == nil {
		t.Fatal("时间戳超出偏差应报错")
	}
	if isAntiReplayUnavailable(err) {
		t.Error("时间戳越界不应被当作防重放故障")
	}
}

// TestShortNonceRejected nonce 过短会频繁碰撞，导致正常请求被误判为重放
func TestShortNonceRejected(t *testing.T) {
	const secret = "test-secret"

	SetClientSignSecret([]byte(secret))
	defer SetClientSignSecret(nil)

	c := signedRequest(t, secret, "device-abc", "short")

	if err := verifySignature(c, "device-abc", c.GetHeader(SignHeaderName), nil); err == nil {
		t.Error("过短的 nonce 应被拒绝")
	}
}
