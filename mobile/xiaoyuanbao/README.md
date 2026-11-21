# 小元宝

安卓 Flutter App，只提供「平局比赛观察」页面。原网页继续保留。

## 使用

1. 用户自行重启更新后的 `go_server`。默认监听电脑的 `28080` 端口。
2. 安装 `build/app/outputs/flutter-apk/app-release.apk`（ARM64 安卓 7.0 及以上）。
3. 手机与电脑接入同一局域网，点右上角连接设置，填写 `http://电脑局域网IP:28080`，例如 `http://192.168.1.10:28080`。手机上不要填 `localhost`。
4. 点「保存并同步全部比赛」，等待同步完成。若无法连接，检查后台已启动、地址正确且电脑防火墙允许该局域网连接。
5. 下载完成后可断网使用；再次点同步只下载新增或变化的比赛。

首次同步包含后台可提供的全部日期、竞彩和非竞彩、已完赛和待赛比赛，筛选条件只影响本地显示。下载数据包括比赛详情、排名与已赛、战绩、各公司赔率、指数、盈亏与支持率、模型推荐及结果。全量下载可能需要几分钟；同步进度会显示新增/变更场数。

本次真库核验共 8,710 场，完整原始数据约 380 MiB，轻量卡片索引约 15 MiB；传输启用 gzip 压缩。手机建议至少留出 1 GB 空间用于完整缓存和同步暂存。卡片只加载摘要，展开后才读取完整本地明细。

断网、下载失败、取消和校验失败均保留上次完整缓存。新一批数据验证完整后才原子切换；服务器删除的比赛同步删除。不同后台地址的缓存分别保存。首次没有缓存时会明确提示连接后台，不显示演示比赛。球队使用本地文字标识，不依赖联网图片。

## 数据与模型口径

- 入选平沿用后台当前模型；竞彩同时校验至少一场有效历史交锋的资格。
- 胜负次选沿用网页规则：入选后，以即时平均欧赔的主/客较低方作为次选；同价或无有效报价显示暂无。
- 大小球独立使用后台的大小球推荐；只有确认的常规时间赛果才结算，四分之一盘口区分全赢、赢半、走盘、输半和全输。
- 威廉−体彩差值为双方即时胜平负赔率相减，非支持率百分比。
- 页面提供日期、全部日期、竞彩/全部赛事、入选状态、完赛状态、联赛及球队搜索。
- 模型版本与最后同步时间在连接设置内显示。离线推荐代表最后同步时的版本，更新后台后需要再次同步 App。

## 构建与验证

使用 Flutter 3.41.2 / Dart 3.11，依赖锁定于 `pubspec.lock`：

```powershell
flutter pub get
flutter analyze
flutter test
flutter build apk --release --target-platform android-arm64
```

安装包用于个人局域网安装，当前使用开发签名。正式商店发布前需配置自己的长期签名密钥。App 不申请外部存储权限，SQLite 缓存在自己的私有目录内。

Android target SDK 36 使用 INTERNET 权限连接手动填写的局域网 HTTP 地址。升级 target SDK 到 37 时需同时实现 ACCESS_LOCAL_NETWORK 运行时授权，参见 [Android 官方说明](https://developer.android.com/privacy-and-security/local-network-permission)。

## Logo

`assets/brand/logo.svg` 是原创可编辑矢量源：深蓝底、金色元宝和足球纹样。`assets/images/logo.png` 用于 App，Android 多分辨率和自适应桌面图标已生成。

修改矢量后可以用 `tool/render-logo.cjs`（Playwright，路径可通过 PLAYWRIGHT_MODULE 指定）和 `tool/launcher-icons.py`（Pillow）重建图标。构建 App 本身不需要这两项工具。

## 后台同步接口

- `POST /api/mobile/sync/manifest`：同一数据库快照生成的全量比赛清单、每场 SHA-256、快照及模型版本。
- `POST /api/mobile/sync/batch`：提交 `{ "snapshot": "…", "ids": ["…"] }`，返回该固定快照的数据；客户端按 SHA-256 验证。
- `GET /api/draw/model/status`：后台启动时模型更新状态和训练截止日期。

后台快照默认保存在 `go_server/data/mobile-sync`（相对于后台工作目录的 `data/mobile-sync`），可通过 `MOBILE_SYNC_DIR` 指定。模型更新读取现有数据库中的最新已完赛数据，外部抓取仍由原采集程序负责。
