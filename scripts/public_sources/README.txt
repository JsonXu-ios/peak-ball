国内备选源独立模块

入口
  后台菜单：国内备选源 → 采集任务 / 比赛分析
  /public-sources/tasks
  /public-sources/matches

隔离范围
  新模块仅使用 data/public-sources/public_sources.sqlite3。
  表：ps_matches、ps_snapshots、ps_jobs、ps_company_mappings、ps_model_results。
  Python进程自行初始化这些表，不执行原MySQL迁移，不读写VIPC业务表。
  原爬虫、go_crawler、go_server、原统计分析菜单继续使用原数据源。
  新模块的比赛分析直接读取新库，选平、二次筛选、两比分与大小球调用原模型纯函数。
  次选胜负及五项推荐展示复用原观察页组件。原观察页的数据源不切换。
  此数据库及运行快照已加入gitignore，备份时请连同SQLite WAL一并处理，
  或使用sqlite3 backup API制作一致备份。

已经导入的核对数据
  2026-11-14，周六003，日本—巴西。
  这是此前真实采集的数据，保留原采集时间，不是当前实时刷新结果。
  独立ID sporttery:2026-11-14:2042753；足彩网ID4650133。
  以后可在详情中修改比赛映射、公司标签映射，或补采本场。

运行环境
  Python 3.10+（开发环境已安装依赖）：
    python -m pip install -r scripts/public_sources/requirements.txt
  动态页面使用专用无界面的Edge进程，默认 channel=msedge；不使用用户浏览器档案。
  不修改、不关闭、不重启用户已有浏览器或任何项目服务。
  无Edge的机器可安装Playwright Chromium：
    python -m playwright install chromium
    设置 PUBLIC_SOURCE_BROWSER_CHANNEL=chromium

管理API进程环境变量（按需设置，设置后由用户自行重启go_admin）
  PUBLIC_SOURCE_PYTHON：Python可执行文件，默认python。
  PUBLIC_SOURCE_WORKER：worker.py的绝对路径。默认自动查找工作区脚本。
  PUBLIC_SOURCE_DB：独立SQLite的绝对路径。默认工作区data/public-sources/public_sources.sqlite3。
  PUBLIC_SOURCE_MODEL：独立离线模型可执行文件绝对路径。
    默认工作区data/public-sources/public-source-model.exe（Windows）或public-source-model（Linux）。
    该程序只读stdin计算并输出JSON，不连接MySQL/Redis，不访问VIPC或启动服务。
  PUBLIC_SOURCE_INTERVAL：顶层请求最短间隔秒数，默认1.5，最低1。
  PUBLIC_SOURCE_PROXIES：真实HTTP(S)代理URL的JSON数组，默认[]，不会伪造X-Forwarded-For。
    例：["http://127.0.0.1:18001","http://127.0.0.1:18002"]
    配置的代理必须由用户提供；没有配置时使用当前网络出口。
    HTTP请求及新浏览器上下文轮换配置的代理；浏览器页面的资源请求、分页复用该上下文出口。
  PUBLIC_SOURCE_BROWSER：默认auto，HTTP没有有效数据时尝试正常浏览器渲染；off仅HTTP。
  PUBLIC_SOURCE_BROWSER_CHANNEL：默认msedge；也可设chromium。
  PUBLIC_SOURCE_KELLY_LABELS：额外读取最早有效历史凯利的来源公司标签。
    默认：威*,36*,平*,官方(胜平负)。设为单独一个*可读取全部公司，采集耗时会增加。
    来源遮罩标签不自动当成已确认公司名称，需在页面保存公司映射。

任务行为
  手动按日期发起采集，可设置1—50场上限，默认10。
  支持接口分页与正常页面分页；不解验证码、不执行安全挑战。
  足彩网比赛自动匹配要求双方全名一致、结果唯一；否则在详情填足彩网比赛ID。
  关联前核对双方球队、日期及已知开赛时间；不做有歧义的模糊关联。
  欧赔、亚盘、大小球、战绩分别保存快照；某项失败不会覆盖原有成功记录。
  任务状态区分完成、部分完成、失败与中断，错误显示在采集任务页面。
  未配置可用代理或遇到来源安全拦截时，不能保证自动采集成功；记录实际错误，不伪造成功。
  后台一次仅执行一个独立采集任务，任务超时30分钟，过期任务可重新发起。
  本版未添加定时任务，点击采集才开始；不会自动启动原VIPC任务。

数据与计算口径
  盈亏率=(1-用户投票支持比例×该页配对赔率)×100%，支持比例先转为0—1。
  冷热差=用户投票支持率-奖金对应概率，单位为百分点。
  这两个值是本地测算，不标为真实资金成交或VIPC原始盈亏/冷热。
  投票与配对赔率一起计算；奖金详情较新时单列显示，不跨时间静默混合。
  最早凯利按来源记录时间排序，不能直接取最新在前的数组第0项。
  本地截止时间采用北京时间开赛时刻，不作为来源官方停售标记。
  实时比分、角球、加时、点球不作为采集必需项；积分榜/联赛统计不阻塞任务。
  缺失值保留为空，不填0。数据详情可查看原始快照、采集时间与来源。

模型结果与筛选
  比赛列表和详情均展示：入选平、次选胜负、二次筛选、两比分、大小球。
  列表可筛选入选平、二次保留、未入选、待计算，并显示各类场数。
  模型输入固定取各类数据最后一份可核对时间的赛前快照。
  开赛之后新采集的数据仅更新原文/报价展示，不替换模型赛前输入。
  战绩表按日期、球队和完整比分解析；排除本场、同日无明确完成时间的比赛、未来记录及重复记录。
  名称/盘口通过独立转换层进入原模型契约，内部兼容编号不是来源ID，也不代表读过原VIPC表。
  入选平沿用原模型门槛和至少1场有效交锋要求；二次保留沿用真实初即时报价与0.225门槛。
  大小球独立于是否入选平；两比分仅在二次保留之后提供。
  结果缓存在ps_model_results，赛前快照、公司映射或模型程序变化时自动重新计算。
  不采集实时赛果，因此新菜单不标注命中/未命中，避免把未知比分当0:0。
  样本日本—巴西的结果是未入选平，大小球为大3球；这不是模型功能缺失。

构建独立模型（无需重启go_server）
  在go_server目录执行：
    Windows: go build -o ../data/public-sources/public-source-model.exe ./cmd/public-source-model
    Linux:   go build -o ../data/public-sources/public-source-model ./cmd/public-source-model
  原模型、特征提取或推荐规则代码更新后，应重新构建此程序。
  本次已完成Windows构建。Python会为每次待更新批次调用离线程序，不需要常驻新服务。

本地命令（在工作区根目录）
  查询：
    将 {"date":"2026-11-14"} 通过stdin传入：
    python scripts/public_sources/worker.py --db data/public-sources/public_sources.sqlite3 --action overview
  导入此前的单场核对样本（仅CLI，不对外开放任意文件读取）：
    python scripts/public_sources/worker.py --db data/public-sources/public_sources.sqlite3 --action import-capture --capture-dir reports/source-probes/zgzcw-sporttery/20261114-single-match
  测试：
    python -m unittest discover -s scripts/public_sources -p test_public_sources.py -v
    在go_admin目录执行 go test ./handlers ./routes
    在admin目录执行 npm run build

上线/使用
  修改已完成构建，不替换正在运行的服务进程。
  用户自行重启go_admin；前端开发服务通常由热更新加载，刷新浏览器即可。
  静态部署使用新admin/dist；本次构建已生成该目录。
  本次UI核验使用临时本地预览、独立数据库副本与专用测试登录，未修改生产登录或鉴权。
