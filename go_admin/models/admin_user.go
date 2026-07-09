// Package models defines database models for admin system.
package models

import (
	"time"

	"gorm.io/gorm"
)

// AdminUser represents an admin user in the system
type AdminUser struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	Username  string         `gorm:"uniqueIndex;size:50;not null" json:"username"`
	Password  string         `gorm:"size:255;not null" json:"-"`
	Nickname  string         `gorm:"size:100" json:"nickname"`
	Email     string         `gorm:"size:100" json:"email"`
	Phone     string         `gorm:"size:20" json:"phone"`
	Avatar    string         `gorm:"size:255" json:"avatar"`
	Status    int            `gorm:"default:1" json:"status"` // 1: active, 0: disabled
	LastLogin *time.Time     `json:"last_login"`
	Roles     []Role         `gorm:"many2many:admin_user_roles;" json:"roles"`
}

// Role represents a role in RBAC
type Role struct {
	ID          uint           `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	Name        string         `gorm:"uniqueIndex;size:50;not null" json:"name"`
	Code        string         `gorm:"uniqueIndex;size:50;not null" json:"code"`
	Description string         `gorm:"size:255" json:"description"`
	Sort        int            `gorm:"default:0" json:"sort"`
	Status      int            `gorm:"default:1" json:"status"`
	Menus       []Menu         `gorm:"many2many:role_menus;" json:"menus,omitempty"`
	Permissions []Permission   `gorm:"many2many:role_permissions;" json:"permissions,omitempty"`
}

// Menu represents a menu item in the admin system
type Menu struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	ParentID  uint           `gorm:"default:0" json:"parent_id"`
	Name      string         `gorm:"size:50;not null" json:"name"`
	Title     string         `gorm:"size:100;not null" json:"title"`
	Icon      string         `gorm:"size:100" json:"icon"`
	Path      string         `gorm:"size:200" json:"path"`
	Component string         `gorm:"size:200" json:"component"`
	Sort      int            `gorm:"default:0" json:"sort"`
	Status    int            `gorm:"default:1" json:"status"`
	MenuType  string         `gorm:"size:20;default:'menu'" json:"menu_type"` // menu, button
	Hidden    bool           `gorm:"default:false" json:"hidden"`
	Children  []Menu         `gorm:"-" json:"children,omitempty"`
}

// Permission represents a permission in RBAC
type Permission struct {
	ID          uint           `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	Name        string         `gorm:"size:100;not null" json:"name"`
	Code        string         `gorm:"uniqueIndex;size:100;not null" json:"code"`
	Description string         `gorm:"size:255" json:"description"`
	Category    string         `gorm:"size:50" json:"category"`
	Status      int            `gorm:"default:1" json:"status"`
}

// RoleMenu is the join table for role and menu
type RoleMenu struct {
	RoleID uint `gorm:"primarykey" json:"role_id"`
	MenuID uint `gorm:"primarykey" json:"menu_id"`
}

// RolePermission is the join table for role and permission
type RolePermission struct {
	RoleID       uint `gorm:"primarykey" json:"role_id"`
	PermissionID uint `gorm:"primarykey" json:"permission_id"`
}

// AdminUserRole is the join table for admin user and role
type AdminUserRole struct {
	AdminUserID uint `gorm:"primarykey" json:"admin_user_id"`
	RoleID      uint `gorm:"primarykey" json:"role_id"`
}

// OperationLog represents an operation log entry
type OperationLog struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	UserID     uint      `json:"user_id"`
	Username   string    `gorm:"size:50" json:"username"`
	Method     string    `gorm:"size:10" json:"method"`
	Path       string    `gorm:"size:200" json:"path"`
	IP         string    `gorm:"size:50" json:"ip"`
	UserAgent  string    `gorm:"size:500" json:"user_agent"`
	StatusCode int       `json:"status_code"`
	Latency    int64     `json:"latency"` // milliseconds
	Request    string    `gorm:"type:text" json:"request"`
	Response   string    `gorm:"type:text" json:"response"`
}
