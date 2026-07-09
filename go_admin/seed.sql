-- Admin Seed Data
-- 初始化管理后台的初始数据

-- 1. 创建超级管理员 (密码: admin123)
-- bcrypt hash of 'admin123'
INSERT INTO admin_users (username, password, nickname, email, status, created_at, updated_at) VALUES
('admin', '$2a$10$JDb8xeUT/LhHEK4JSGwoYuo5iP/5hNSaV.D2JxuvyOj0XBhK8/XbO', '超级管理员', 'admin@football.com', 1, NOW(), NOW()),
('editor', '$2a$10$JDb8xeUT/LhHEK4JSGwoYuo5iP/5hNSaV.D2JxuvyOj0XBhK8/XbO', '编辑员', 'editor@football.com', 1, NOW(), NOW()),
('viewer', '$2a$10$JDb8xeUT/LhHEK4JSGwoYuo5iP/5hNSaV.D2JxuvyOj0XBhK8/XbO', '查看员', 'viewer@football.com', 1, NOW(), NOW())
ON DUPLICATE KEY UPDATE password = VALUES(password), nickname = VALUES(nickname), email = VALUES(email), status = VALUES(status), updated_at = NOW();

-- 2. 创建角色
INSERT INTO roles (name, code, description, sort, status, created_at, updated_at) VALUES
('超级管理员', 'super_admin', '拥有所有权限', 1, 1, NOW(), NOW()),
('内容管理员', 'content_admin', '管理新闻和内容', 2, 1, NOW(), NOW()),
('数据管理员', 'data_admin', '管理爬虫和比赛数据', 3, 1, NOW(), NOW()),
('只读用户', 'viewer', '只能查看数据', 4, 1, NOW(), NOW())
ON DUPLICATE KEY UPDATE updated_at = NOW();

-- 清理已取消菜单
DELETE FROM role_menus WHERE menu_id IN (SELECT id FROM menus WHERE path LIKE '/prediction%');
DELETE FROM menus WHERE path LIKE '/prediction%';

-- 3. 创建菜单
INSERT INTO menus (parent_id, name, title, icon, path, component, sort, status, menu_type, hidden, created_at, updated_at) VALUES
-- 一级菜单
(0, 'Dashboard', '仪表盘', 'mdi-view-dashboard', '/dashboard', 'views/dashboard/DashboardView', 1, 1, 'menu', 0, NOW(), NOW()),
(0, 'System', '系统管理', 'mdi-cog', '/system', 'layouts/RouteView', 2, 1, 'menu', 0, NOW(), NOW()),
(0, 'Crawler', '爬虫管理', 'mdi-spider-web', '/crawler', 'layouts/RouteView', 3, 1, 'menu', 0, NOW(), NOW()),
(0, 'Data', '数据管理', 'mdi-database', '/data', 'layouts/RouteView', 4, 1, 'menu', 0, NOW(), NOW()),
-- 系统管理子菜单 (parent_id = 2)
(2, 'UserManage', '用户管理', 'mdi-account-multiple', '/system/users', 'views/system/UserManage', 1, 1, 'menu', 0, NOW(), NOW()),
(2, 'RoleManage', '角色管理', 'mdi-shield-account', '/system/roles', 'views/system/RoleManage', 2, 1, 'menu', 0, NOW(), NOW()),
(2, 'MenuManage', '菜单管理', 'mdi-menu', '/system/menus', 'views/system/MenuManage', 3, 1, 'menu', 0, NOW(), NOW()),
(2, 'PermManage', '权限管理', 'mdi-key', '/system/permissions', 'views/system/PermissionManage', 4, 1, 'menu', 0, NOW(), NOW()),
(2, 'OperationLog', '操作日志', 'mdi-history', '/system/logs', 'views/system/OperationLog', 5, 1, 'menu', 0, NOW(), NOW()),
-- 爬虫管理子菜单 (parent_id = 3)
(3, 'CrawlerTask', '爬虫任务', 'mdi-robot', '/crawler/tasks', 'views/crawler/CrawlerTask', 1, 1, 'menu', 0, NOW(), NOW()),
(3, 'CrawlerLog', '爬虫日志', 'mdi-text-box-outline', '/crawler/logs', 'views/crawler/CrawlerLog', 2, 1, 'menu', 0, NOW(), NOW()),
(3, 'CrawlerSync', '数据同步', 'mdi-sync', '/crawler/sync', 'views/crawler/CrawlerSync', 3, 1, 'menu', 0, NOW(), NOW()),
-- 数据管理子菜单 (parent_id = 4)
(4, 'MatchData', '比赛数据', 'mdi-soccer', '/data/matches', 'views/data/MatchData', 1, 1, 'menu', 0, NOW(), NOW()),
(4, 'HistoryData', '历史数据', 'mdi-chart-timeline', '/data/history', 'views/data/HistoryData', 2, 1, 'menu', 0, NOW(), NOW()),
(4, 'OddsData', '赔率数据', 'mdi-chart-line', '/data/odds', 'views/data/OddsData', 3, 1, 'menu', 0, NOW(), NOW())
ON DUPLICATE KEY UPDATE updated_at = NOW();

-- 4. 创建权限
INSERT INTO permissions (name, code, description, category, status, created_at, updated_at) VALUES
-- 系统权限
('查看用户', 'system:user:list', '查看用户列表', 'system', 1, NOW(), NOW()),
('创建用户', 'system:user:create', '创建新用户', 'system', 1, NOW(), NOW()),
('编辑用户', 'system:user:update', '编辑用户信息', 'system', 1, NOW(), NOW()),
('删除用户', 'system:user:delete', '删除用户', 'system', 1, NOW(), NOW()),
('查看角色', 'system:role:list', '查看角色列表', 'system', 1, NOW(), NOW()),
('管理角色', 'system:role:manage', '创建/编辑/删除角色', 'system', 1, NOW(), NOW()),
('查看菜单', 'system:menu:list', '查看菜单列表', 'system', 1, NOW(), NOW()),
('管理菜单', 'system:menu:manage', '创建/编辑/删除菜单', 'system', 1, NOW(), NOW()),
-- 爬虫权限
('查看爬虫任务', 'crawler:task:list', '查看爬虫任务列表', 'crawler', 1, NOW(), NOW()),
('管理爬虫任务', 'crawler:task:manage', '创建/编辑/删除爬虫任务', 'crawler', 1, NOW(), NOW()),
('运行爬虫', 'crawler:task:run', '手动运行爬虫任务', 'crawler', 1, NOW(), NOW()),
('查看爬虫日志', 'crawler:log:list', '查看爬虫执行日志', 'crawler', 1, NOW(), NOW()),
('数据同步', 'crawler:sync', '发起数据同步', 'crawler', 1, NOW(), NOW()),
-- 数据权限
('查看比赛数据', 'data:match:list', '查看比赛数据', 'data', 1, NOW(), NOW()),
('删除比赛数据', 'data:match:delete', '删除比赛数据', 'data', 1, NOW(), NOW()),
('查看赔率数据', 'data:odds:list', '查看赔率数据', 'data', 1, NOW(), NOW()),
('查看操作日志', 'system:log:list', '查看操作日志', 'system', 1, NOW(), NOW())
ON DUPLICATE KEY UPDATE updated_at = NOW();

-- 5. 分配角色给用户
INSERT INTO admin_user_roles (admin_user_id, role_id) VALUES
(1, 1),  -- admin -> super_admin
(2, 2),  -- editor -> content_admin
(3, 4)   -- viewer -> viewer
ON DUPLICATE KEY UPDATE role_id = role_id;

-- 6. 分配菜单给角色 (超级管理员拥有所有菜单)
INSERT INTO role_menus (role_id, menu_id)
SELECT 1, id FROM menus
ON DUPLICATE KEY UPDATE menu_id = menu_id;

-- 7. 分配权限给角色 (超级管理员拥有所有权限)
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions
ON DUPLICATE KEY UPDATE permission_id = permission_id;

-- 8. 初始化爬虫任务
INSERT INTO crawler_tasks (name, type, status, schedule, config, description, is_enabled, created_at, updated_at) VALUES
('每日比赛列表', 'match_list', 'pending', '0 8 * * *', '{}', '每天早上8点拉取比赛列表、比分状态和队徽；config 不填 date 时包含今日和明日，填 date 时只跑指定日期', 1, NOW(), NOW()),
('历史战绩', 'history', 'pending', '0 9 * * *', '{}', '不填 match_id 时按日期批量获取历史战绩；填 match_id 时只跑单场', 1, NOW(), NOW()),
('联赛排名/杯赛积分榜', 'rank', 'pending', '', '{}', '不填 match_id 时按日期批量获取排名/积分榜；填 match_id 时只跑单场', 1, NOW(), NOW()),
('欧赔数据', 'odds_euro', 'pending', '0 10 * * *', '{}', '不填 match_id 时按日期批量获取欧赔；填 match_id 时只跑单场', 1, NOW(), NOW()),
('盘口数据', 'odds_pankou', 'pending', '0 11 * * *', '{}', '不填 match_id 时按日期批量获取亚盘和大小球；填 match_id 时只跑单场', 1, NOW(), NOW()),
('阶段赔率盘口刷新', 'odds_refresh', 'pending', '*/30 * * * *', '{}', '临近开赛或赛中强制刷新欧赔、亚盘和大小球，不重拉历史和排名', 0, NOW(), NOW()),
('全量同步', 'all', 'pending', '0 6 * * *', '{}', '每天早上6点执行全量数据同步：比赛列表、历史、排名/积分榜、欧赔、盘口；不填 date 时包含今日和明日', 0, NOW(), NOW())
ON DUPLICATE KEY UPDATE updated_at = NOW();
