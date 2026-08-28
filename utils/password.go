package utils

import (
	"crypto/rand"
	"math/big"

	"golang.org/x/crypto/bcrypt"
)

const (
	lowercaseChars = "abcdefghijklmnopqrstuvwxyz"
	uppercaseChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	numberChars    = "0123456789"
	allChars       = lowercaseChars + uppercaseChars + numberChars
)

// GenerateRandomPassword 生成随机密码，确保至少包含小写、大写和数字各一位
func GenerateRandomPassword(length int) (string, error) {
	if length < 8 {
		length = 8
	}

	password := make([]byte, length)

	// 各类字符至少一位
	for i, charset := range []string{lowercaseChars, uppercaseChars, numberChars} {
		c, err := randomChar(charset)
		if err != nil {
			return "", err
		}
		password[i] = c
	}

	// 填充剩余位置
	for i := 3; i < length; i++ {
		c, err := randomChar(allChars)
		if err != nil {
			return "", err
		}
		password[i] = c
	}

	// 打乱顺序，避免前三位类型固定
	for i := len(password) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		password[i], password[j.Int64()] = password[j.Int64()], password[i]
	}

	return string(password), nil
}

// randomChar 从字符集中随机取一个字符
func randomChar(charset string) (byte, error) {
	idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
	if err != nil {
		return 0, err
	}
	return charset[idx.Int64()], nil
}

// HashPassword 哈希密码
func HashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

// VerifyPassword 验证密码
func VerifyPassword(hashedPassword, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password)) == nil
}
