package models

import (
	"bookfinder-backend/database"
	"bookfinder-backend/types"
)

// GetAdmin 获取唯一管理员
func GetAdmin() (*types.User, error) {
	var user types.User
	if err := database.GetAppDB().Where("role = ?", types.RoleAdmin).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByUsername 根据用户名获取用户
func GetUserByUsername(username string) (*types.User, error) {
	var user types.User
	if err := database.GetAppDB().Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// UpdateAdminPassword 更新管理员密码，password 需为已哈希的值。
// 只允许改密码：用户名与角色固定，管理员身份不可转让。
func UpdateAdminPassword(id int, hashedPassword string) error {
	return database.GetAppDB().
		Model(&types.User{}).
		Where("id = ? AND role = ?", id, types.RoleAdmin).
		Update("password", hashedPassword).Error
}
