# vue_vuetify_V1

这是当前在用的新版项目，工作区中的 vue_vuetify_parseserver_cypress 仅作为旧版参考保留。

## 当前项目总览

完整项目结构、启动流程、今日爬虫执行结果和今日比赛结论文档见：`doc/项目总览与今日执行结果.md`

## 项目结构

- 前台 H5：Vue 3 + Vite，目录 `src/`，默认端口 `5173`
- 管理后台：Vue 3 + Vuetify + Vite，目录 `admin/src/`，默认端口 `5174`
- 前台 API：Go + Gin，目录 `go_server/`，默认端口 `18080`
- 管理 API：Go + Gin，目录 `go_admin/`，默认端口 `8081`
- 独立爬虫：Go，目录 `go_crawler/`
- 数据库：MySQL，默认库名 `football_data`

## 本地前提

- Node.js
- Go 1.25+
- MySQL 8
- 本地数据库存在：`football_data`
- 默认数据库连接：`root:123456@127.0.0.1:3306`

## 建表说明

- 当前仓库没有单独维护 schema.sql
- 前台 API、管理 API、独立爬虫都会在启动时通过 GORM AutoMigrate 自动创建/更新各自需要的表
- `go_server/seed.sql` 和 `go_admin/seed.sql` 只负责插入演示/初始化数据，前提是对应表已经由程序启动时自动建好

## 启动顺序

1. 启动前台 API

```powershell
cd go_server
go run .
```

2. 启动管理 API

```powershell
cd go_admin
go run .
```

3. 初始化管理端种子数据（首次需要）

```powershell
mysql -uroot -p123456 football_data < go_admin/seed.sql
```

4. 启动前台

```powershell
npm install
npm run dev
```

5. 启动管理后台

```powershell
cd admin
npm install
npm run dev
```

## 默认访问地址

- 前台：http://localhost:5173
- 管理后台：http://localhost:5174
- 前台 API：http://localhost:18080
- 管理 API：http://localhost:8081

## 管理后台默认账号

- 用户名：`admin`
- 密码：`admin123`

## 爬虫说明

- 目标站点页面：`https://www.vipc.cn/live/football`
- 实际请求接口：`https://www.vipc.cn/i/...`
- 当前策略：随机 User-Agent、随机 IP 请求头、请求间隔控制
- 已不再使用旧版 Node 项目中的代理隧道方案
- 前台开发环境通过 Vite 代理 `/api`、`/footballimg`、`/images` 到 `18080`

## 常用命令

```powershell
# 前台构建
npm run build

# 管理后台构建
cd admin
npm run build

# 编译 Go 服务
cd ..\go_server
go build ./...

cd ..\go_admin
go build ./...

cd ..\go_crawler
go build ./...
```