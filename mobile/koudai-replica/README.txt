口袋牛店客户端独立安装测试副本

交付方式
基于用户提供并从其手机提取的 1.1.79 APK 重打包。
保留原有客户端页面、业务脚本、DEX、原生库和服务链接。
这是可独立安装的客户端副本，不是原始 Android/后端源码的恢复。

安装标识
原版：com.niudian.yun.cordova.koudai
副本：com.niudian.yun.cordova.koudai.replica
副本名称：口袋牛店·独立测试
副本专用链接协议：koudai-replica

构建
在仓库根目录执行：
  python mobile/koudai-replica/repackage_manifest.py
  python mobile/koudai-replica/sign_and_verify.py mobile/koudai-replica/work/koudai-replica-unsigned.apk

默认读取：reports/source-probes/koudai-mobile-20261119/base.apk
其他 APK 可通过 --source 指定，但脚本会核对源包名；不能任意替换其他应用。
Android JDK、build-tools 的位置可通过 --jdk、--build-tools 指定。
构建不启动或重启原项目，也不会安装或启动手机应用。

产物
out/koudai-replica-1.1.79.apk：已对齐并使用独立测试证书签名的 APK。
out/verification.json：文件内容、签名、对齐、安装标识核验。
work/prototype-report.json：安装标识的具体变更。
private/：本地测试签名文件，不提交到版本库、不分发给使用者。

账号与服务
账号密码不放进脚本、资源、安装包或构建配置。
副本使用独立的 Android 应用数据目录；不会复制原 App 的登录状态或订单缓存。
服务仍由原平台提供，账号权限、服务可达性和会员功能取决于原平台。
不改动登录校验、会员校验、TLS 校验或应用的安全校验。

验证范围
静态构建通过仅表示 APK 格式、签名、安装标识和客户端文件保持情况通过。
必须安装后验证冷启动、隐私流程、普通账号登录、店铺、比赛列表、比赛分析、
盈亏及其他导航；不能将静态存在的 318 条路由称为全部功能验证通过。
一键登录、微信/QQ回调、推送、地图、广告等原生服务可能绑定原包名或签名。
原版链接 koudai:// 仍归原应用；副本的独立链接为 koudai-replica://。
当前工程不会自动执行支付、下单、发送消息、更新或覆盖原应用。

后续维护
如果服务或 SDK 明确拒绝新包名/签名，应记录并向对应服务申请配置，
不把移除原校验当作修复。原 App 的自动更新包也不能当作副本的签名更新。

实际接口核查（2026-11-19）
已按本次用户授权更新并启动诊断测试版；只在副本 Manifest 增加 debuggable。
构建命令：
  python mobile/koudai-replica/add_debuggable.py
  python mobile/koudai-replica/sign_and_verify.py mobile/koudai-replica/work/koudai-replica-debuggable-unsigned.apk --diagnostic
安装、启动或重启不包含在这些命令内；遵守根目录 AGENTS.md 的重启约定。

capture_network.py 通过已开启的 WebView 调试通道观察 Network 事件并读取响应。
不重放请求、不修改 TLS/鉴权，也不记录 Cookie、原始头部或原始 POST。
比赛接口保留脱敏 JSON，账号及其他业务接口仅保留字段结构。
每轮使用新的空输出目录；写入该目录的 STOP 文件可正常结束并生成 summary.json。
端口转发需指向当前测试版 PID 的 webview_devtools socket，采集结束后移除本轮映射。
不得据此将未访问页面或原生 SDK 流量称为已验证。

本轮结果：210 次响应，47 个不同地址，其中45个 JSON 接口、2个图片地址。
静态 APK 清单598个候选与本轮实际调用分别统计，不能相互替代。
报告：reports/source-probes/koudai-mobile-20261119/api-audit-confirmed.txt
字段与证据：reports/source-probes/koudai-mobile-20261119/api-audit-confirmed.json
脱敏响应：reports/source-probes/koudai-mobile-20261119/runtime-api-02/responses.jsonl
验证采集器：python -m unittest discover -s mobile/koudai-replica -p test_capture_network.py -v
