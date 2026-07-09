# 足球数据管理后台

基于 **Vue 3 + Vuetify 3 + Pinia** (前端) 和 **Go Gin + GORM + MySQL** (后端) 的完整管理后台系统。

## 系统架构

```
admin/          → 前端 Vue 3 + Vuetify 3 管理后台 (端口 5174)
go_admin/       → 后端 Go Gin Admin API 服务 (端口 8081)
go_crawler/     → 数据爬虫 (已有)
go_server/      → 前台 API 服务 (已有, 端口 8080)
```

## 功能模块

### 🔐 系统管理
- **用户管理** — CRUD、启用/禁用、重置密码、角色分配
- **角色管理** — RBAC角色、菜单分配、权限分配
- **菜单管理** — 动态菜单配置、树形结构
- **权限管理** — 细粒度权限编码（如 `system:user:list`）
- **操作日志** — 请求方法/路径/IP/耗时追踪

### 🕷️ 爬虫管理
- **爬虫任务** — 任务卡片管理、启用/禁用、手动执行
- **爬虫日志** — 执行状态/耗时/数据量/错误信息
- **数据同步** — 手动触发同步（同步/异步模式），支持：
  - 比赛列表采集
  - 历史战绩采集
  - 欧赔数据采集
  - 盘口数据采集
  - 全量数据同步

### 📊 数据管理
- **比赛数据** — 浏览/搜索/筛选/删除爬虫数据
- **按联赛/日期筛选** — 下拉联赛和日期列表
- **详情查看** — 查看比赛基础信息/历史/赔率/盘口 JSON 数据

### 📈 仪表盘
- 统计卡片（总比赛数/今日比赛/用户数/爬虫成功失败率）
- 联赛数据分布
- 最近爬虫日志

## 快速开始

### 1. 初始化数据库

确保 MySQL 已运行，数据库 `football_data` 已创建：

```sql
CREATE DATABASE IF NOT EXISTS football_data;
```

### 2. 启动 Go 后端

```bash
cd go_admin
go mod tidy
go run main.go
```

服务器将在 `http://localhost:8081` 启动，首次运行会自动创建表结构。

### 3. 导入初始数据

```bash
mysql -u root -p123456 football_data < go_admin/seed.sql
```

### 4. 启动前端

```bash
cd admin
npm install
npm run dev
```

访问 `http://localhost:5174`

### 5. 默认登录

| 账号 | 密码 | 角色 |
|------|------|------|
| admin | admin123 | 超级管理员 |
| editor | admin123 | 内容管理员 |
| viewer | admin123 | 只读用户 |

> ⚠️ **注意**: 首次使用 seed.sql 前需要先启动一次 go_admin 服务让 GORM 自动建表，然后再导入 seed.sql。

## 技术栈

### 前端
- **Vue 3** — Composition API + `<script setup>`
- **Vuetify 3** — Material Design UI 组件库
- **Pinia** — 状态管理
- **Vue Router 4** — 路由 + 导航守卫
- **Axios** — HTTP 请求 + 拦截器
- **Vite** — 构建工具

### 后端
- **Go Gin** — HTTP 框架
- **GORM** — ORM
- **MySQL** — 数据库
- **JWT** — 认证
- **bcrypt** — 密码加密

## API 端点

### 公开接口
| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/login` | 管理员登录 |

### 认证保护接口 (`/api/admin/`)
| 模块 | 端点数 | 说明 |
|------|--------|------|
| 用户管理 | 7 | CRUD + 状态 + 密码重置 |
| 角色管理 | 8 | CRUD + 菜单/权限分配 |
| 菜单管理 | 5 | CRUD + 树形结构 |
| 权限管理 | 4 | CRUD |
| 爬虫数据 | 3 | 查看/详情/删除 |
| 爬虫任务 | 6 | CRUD + 运行/开关 |
| 爬虫日志 | 2 | 列表/详情 |
| 爬虫同步 | 1 | 触发同步 |
| 操作日志 | 1 | 列表 |
| 仪表盘 | 1 | 统计数据 |

**合计: 38 个管理后台 API 端点**

## 目录结构

```
admin/                          # 前端项目
├── src/
│   ├── api/                    # API 请求层
│   │   ├── request.ts          # Axios 实例 + 拦截器
│   │   └── index.ts            # 所有 API 函数
│   ├── layouts/                # 布局组件
│   │   └── AdminLayout.vue     # 管理后台主布局
│   ├── plugins/                # 插件
│   │   └── vuetify.ts          # Vuetify 配置 (暗色/亮色主题)
│   ├── router/                 # 路由配置
│   │   └── index.ts            # 路由 + 导航守卫
│   ├── store/                  # Pinia Store
│   │   └── auth.ts             # 认证状态管理
│   └── views/                  # 页面组件
│       ├── LoginView.vue       # 登录页
│       ├── dashboard/          # 仪表盘
│       ├── system/             # 系统管理 (用户/角色/菜单/权限/日志)
│       ├── crawler/            # 爬虫管理 (任务/日志/同步)
│       └── data/               # 数据管理 (比赛数据)

go_admin/                       # 后端项目
├── config/                     # 配置
├── database/                   # 数据库连接/迁移
├── handlers/                   # HTTP 处理器
│   ├── auth.go                 # 登录 + 用户信息
│   ├── user.go                 # 用户管理
│   ├── role.go                 # 角色管理
│   ├── menu.go                 # 菜单管理
│   ├── permission.go           # 权限管理
│   ├── crawler.go              # 爬虫数据 + 同步
│   ├── crawler_task.go         # 爬虫任务
│   ├── dashboard.go            # 仪表盘统计
│   └── operation_log.go        # 操作日志
├── middleware/                  # 中间件
│   └── jwt.go                  # JWT 认证
├── models/                     # 数据模型
│   ├── admin_user.go           # 管理用户/角色/菜单/权限
│   └── crawler.go              # 爬虫任务/日志/数据
├── routes/                     # 路由注册
├── utils/                      # 工具函数
│   └── auth.go                 # JWT + bcrypt
├── seed.sql                    # 初始数据
└── main.go                     # 入口
```
