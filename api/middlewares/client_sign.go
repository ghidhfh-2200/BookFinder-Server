package middlewares

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"bookfinder-backend/database"
	"bookfinder-backend/logger"
	"bookfinder-backend/utils"

	"github.com/gin-gonic/gin"
)

// 安卓客户端签名相关的请求头与上下文键
const (
	// DeviceHeaderName 客户端上报的设备标识
	DeviceHeaderName = "X-BF-Device"
	// TimestampHeaderName 请求的 Unix 秒时间戳
	TimestampHeaderName = "X-BF-Ts"
	// NonceHeaderName 本次请求的随机数，用于防重放
	NonceHeaderName = "X-BF-Nonce"
	// SignHeaderName 请求签名
	SignHeaderName = "X-BF-Sign"
	// DeviceKeyContextKey 上下文中设备标识哈希的键。
	// 只有签名校验通过才会写入：未验签的设备标识一律不采信。
	DeviceKeyContextKey = "device_key"
)

const (
	// signSkew 允许的时间偏差。
	// 放宽到五分钟以容忍手机时钟不准，同时把重放窗口限制在这个范围内。
	signSkew = 5 * time.Minute
	// nonceTTL nonce 的留存时长，须大于 signSkew 的两倍，
	// 否则时间窗内的旧 nonce 会因过期而重新可用
	nonceTTL = 15 * time.Minute
	// maxSignedBodyBytes 参与签名的请求体上限，与全局请求体上限一致
	maxSignedBodyBytes = 64 << 10
)

// signSecret 客户端签名密钥，为空表示该层关闭
var signSecret []byte

// SetClientSignSecret 设置客户端签名密钥。
// 留空则不校验签名，此时一律不采信客户端上报的设备标识。
func SetClientSignSecret(secret []byte) {
	signSecret = secret
}

// ClientSignMiddleware 校验安卓客户端的请求签名，通过后才采信其上报的设备标识。
//
// 为什么需要签名：设备标识是「卸载重装、换 IP 后封禁仍跟着」的唯一依据，
// 但它由客户端算出后上报。若不验签，改一个请求头就能换一个新身份，
// 更能拿别人的设备标识来污染他人——封禁会落到无关的人头上。
//
// 侧载分发的现实限制：密钥内置在 APK 里，逆包即可取出。因此这一层的作用是
// 把伪造设备标识的门槛从「改一个请求头」抬到「逆包取密钥」，而非不可破解。
// 真正的抗规避主力是 IP 层判据与资源封顶。
//
// 未携带签名头的请求（浏览器）照常放行，只是不带设备标识——
// 浏览器端的身份依据是令牌与 IP。
func ClientSignMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if len(signSecret) == 0 {
			c.Next()
			return
		}

		device := c.GetHeader(DeviceHeaderName)
		sign := c.GetHeader(SignHeaderName)

		// 浏览器不带这些头，照常放行
		if device == "" && sign == "" {
			c.Next()
			return
		}

		// 只带其中一个说明是构造出来的请求，拒绝而非降级：
		// 降级会给出「去掉签名头即可绕过设备标识」的现成办法
		if device == "" || sign == "" {
			utils.ResponseError(c, http.StatusBadRequest, "请求签名不完整")
			c.Abort()
			return
		}

		body, err := readBodyForSigning(c)
		if err != nil {
			utils.ResponseError(c, http.StatusBadRequest, "请求体读取失败")
			c.Abort()
			return
		}

		if err := verifySignature(c, device, sign, body); err != nil {
			// 防重放设施不可用是我方故障，不是伪造的证据：此时降级为
			// 「不采信设备标识」而非拒绝服务。否则打掉 Redis 就能让整个安卓端
			// 不可用——那把一个可用性问题变成了更严重的可用性问题。
			//
			// 代价是这段时间内的请求不带设备标识，身份依据退回令牌与 IP；
			// 重放窗口也回到 signSkew 那么大。两者都好过整端不可用。
			if isAntiReplayUnavailable(err) {
				logger.Warnf("防重放不可用，本次请求不采信设备标识 (%s %s): %v",
					c.Request.Method, c.Request.URL.Path, err)
				c.Next()
				return
			}

			// 其余情形是伪造或重放的证据：签名不符、时间戳越界、nonce 重复。
			// 不回传具体原因：逐条告知等于给出调试伪造请求的反馈通道
			logger.Warnf("客户端请求签名校验失败 (%s %s): %v",
				c.Request.Method, c.Request.URL.Path, err)
			utils.ResponseError(c, http.StatusUnauthorized, "请求签名无效")
			c.Abort()
			return
		}

		c.Set(DeviceKeyContextKey, utils.HashDeviceID(device))

		c.Next()
	}
}

// errAntiReplayUnavailable 防重放设施不可用（Redis 未连接或登记失败）。
//
// 与「签名不符」「nonce 重复」区分开：后者是伪造或重放的证据，应当拒绝；
// 前者是我方故障，拒绝的话打掉 Redis 即可让整个安卓端不可用。
var errAntiReplayUnavailable = errors.New("防重放设施不可用")

// isAntiReplayUnavailable 判断校验失败是否出自防重放设施故障，而非伪造证据。
// 只有这一类失败才降级放行，其余（签名不符、时间戳越界、nonce 重复）一律拒绝。
func isAntiReplayUnavailable(err error) bool {
	return errors.Is(err, errAntiReplayUnavailable)
}

// verifySignature 校验时间戳、nonce 与签名
func verifySignature(c *gin.Context, device, sign string, body []byte) error {
	rawTimestamp := c.GetHeader(TimestampHeaderName)
	nonce := c.GetHeader(NonceHeaderName)

	if rawTimestamp == "" || nonce == "" {
		return fmt.Errorf("缺少时间戳或 nonce")
	}
	// nonce 过短会频繁碰撞，导致正常请求被误判为重放
	if len(nonce) < 16 || len(nonce) > 128 {
		return fmt.Errorf("nonce 长度不合法")
	}

	seconds, err := strconv.ParseInt(rawTimestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("时间戳不是合法整数")
	}

	// 时间窗把重放限制在 signSkew 内，nonce 则挡住窗内的重复提交，
	// 两者缺一不可：只有时间窗则窗内可无限重放，只有 nonce 则记录要永久留存
	skew := time.Since(time.Unix(seconds, 0))
	if skew < -signSkew || skew > signSkew {
		return fmt.Errorf("时间戳超出允许偏差")
	}

	expected := computeSignature(c.Request.Method, c.Request.URL.Path,
		rawTimestamp, nonce, device, body)

	// 恒定时间比较，避免按字节逐位试探签名
	if !hmac.Equal([]byte(sign), []byte(expected)) {
		return fmt.Errorf("签名不符")
	}

	return consumeNonce(c, nonce)
}

// consumeNonce 登记 nonce，已存在则视为重放。
//
// 设施不可用与「确实重放了」要分开报：前者返回 errAntiReplayUnavailable，
// 调用方据此降级为「不采信设备标识」——请求仍可访问，只是不带设备标识；
// 后者是重放的证据，调用方会拒绝该请求。
//
// 之所以不在设施不可用时也拒绝：那样打掉 Redis 就能让整个安卓端不可用。
// 代价是这段时间内重放窗口回到 signSkew 那么大，而设备标识本就不被采信。
func consumeNonce(c *gin.Context, nonce string) error {
	rdb := database.GetRedis()
	if rdb == nil {
		return fmt.Errorf("%w：Redis 未连接", errAntiReplayUnavailable)
	}

	// SetNX 只在键不存在时写入，返回值即「本次是否是首次使用该 nonce」
	fresh, err := rdb.SetNX(c.Request.Context(), "bf:nonce:"+nonce, "1", nonceTTL).Result()
	if err != nil {
		return fmt.Errorf("%w：登记失败: %v", errAntiReplayUnavailable, err)
	}
	if !fresh {
		return fmt.Errorf("nonce 已被使用，疑似重放")
	}

	return nil
}

// computeSignature 计算请求签名。
//
// 方法与路径入签，避免把一个读请求的签名挪用到写请求上；
// 请求体入签，避免在签名不变的前提下改动内容。
func computeSignature(method, path, timestamp, nonce, device string, body []byte) string {
	bodyHash := sha256.Sum256(body)

	mac := hmac.New(sha256.New, signSecret)
	for _, part := range []string{
		method, path, timestamp, nonce, device, hex.EncodeToString(bodyHash[:]),
	} {
		mac.Write([]byte(part))
		mac.Write([]byte("\n"))
	}

	return hex.EncodeToString(mac.Sum(nil))
}

// readBodyForSigning 读出请求体供签名校验，并重新填回供后续处理函数使用
func readBodyForSigning(c *gin.Context) ([]byte, error) {
	if c.Request.Body == nil {
		return nil, nil
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxSignedBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxSignedBodyBytes {
		return nil, fmt.Errorf("请求体超出上限")
	}

	// body 已被读空，重新填回，否则后续 ShouldBindJSON 拿不到内容
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	return body, nil
}
