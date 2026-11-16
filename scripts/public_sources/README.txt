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

当前接口核验样本与清单
  2026-11-15，周日010，西班牙—英格兰；北京时间2026-11-16 03:45开赛。
  足彩网CP比赛ID2042775，通过源页tr_id/newplayid明确关联足彩网分析ID4518112。
  CP、FENXI、体彩的ID空间独立；即使数字碰巧相同也不能省略关联校验。
  请求清单：reports/source-probes/source-split/verify-requests.http。
  Postman导入：reports/source-probes/source-split/verify-postman.json。
  共44个核验示例，包括按需使用的支持比例第2页、24个CP附加数据参数组合。
  每项注明返回类型、用途、实际使用和证据；手动请求保持5—7秒间隔。
  示例固定为已经核验的历史比赛，不表示这些报价仍是当前实时数据。
  reports/source-probes/interface-recheck/verify-requests.http及verify-postman.json
  是旧分工历史清单，已过期；其中体彩比赛头、奖金和战绩请求不属于当前采集流程。

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
  PUBLIC_SOURCE_INTERVAL：顶层请求最短间隔秒数，默认5，最低3，最高60。
  PUBLIC_SOURCE_JITTER：每次随机增加的间隔上限秒数，默认2，范围0—30。
    默认页面与数据接口请求间隔5—7秒，浏览器内部的数据请求也排队降频。
  HTTP请求、每次新打开的浏览器页面生成随机X-Forwarded-For。
    同一个页面的重定向、资源与接口请求保持一致，避免页面加载中途变更请求身份导致反复重定向。
    不重复附加大小写不同的同名请求头；这是请求头，不表示公网出口更换。
  PUBLIC_SOURCE_PROXIES：可选HTTP(S)代理URL的JSON数组，默认[]；不作为采集前提。
  PUBLIC_SOURCE_PROXY_FILE：代理池JSON文件；默认data/public-sources/proxies.json。
    未设置PUBLIC_SOURCE_PROXIES时读取文件；每个新采集任务重新加载，无需为更新文件重启服务。
    任务页面显示随机请求头、主请求间隔及可选代理入口数。
    例：["http://127.0.0.1:18001","http://127.0.0.1:18002"]
    配置的代理必须由用户提供；没有配置时使用当前网络出口。
    HTTP请求及新浏览器上下文轮换配置的代理；浏览器页面的资源请求、分页复用该上下文出口。
  PUBLIC_SOURCE_BROWSER：默认auto，直连接口失败时可用正常浏览器会话请求同一接口，读取HTTP响应JSON；off仅HTTP。
    接口采集不会从浏览器DOM提取JSON，也不会以网页表格替代失败的接口。
  PUBLIC_SOURCE_BROWSER_CHANNEL：默认msedge；也可设chromium。
  PUBLIC_SOURCE_KELLY_LABELS：额外读取最早有效历史凯利的来源公司标签。
    默认：威*,36*,平*,官方(胜平负)。设为单独一个*可读取全部公司，采集耗时会增加。
    来源遮罩标签不自动当成已确认公司名称，需在页面保存公司映射。

任务行为
  手动按日期发起采集，可设置1—50场上限，默认10。
  来源分工固定：体彩只提供支持比例、票数及同一响应中的配对赔率/概率，供盈亏与冷热测算；
  比赛信息、奖金报价包、欧赔、亚盘、大小球、战绩均由足彩网提供，不跨站补缺。
  所有数据先请求已确认的接口；接口超时、结构变化、验证拦截均记为失败，不自动转页面。
  支持率按接口页码分页；同一页或不同页出现重复比赛、其他竞彩日数据时拒绝写入。
  比赛时间从足彩网欧亚指数JSON取得，同时核对竞彩编号、双方球队、竞彩日期。
  两站先按竞彩日期及竞彩编号对应（体彩matchNum、足彩网CC_ID，例如2026-11-15＋周日010），
  再复核主客队与可用开赛时间。接口内部数字ID分别保存，不把各站matchId当成通用主键。
  投票接口自身的全名/简称可用于对应；已有可信开赛时间仅作可选复核，不再额外请求体彩比赛头。
  手工填足彩网ID仍须通过身份校验；已验证的同场映射允许保留球队译名差异，不做模糊关联。
  一个任务复用浏览器会话；主请求、浏览器内部数据请求均降频。
  欧赔、亚盘、大小球、战绩分别保存快照；某项失败不会覆盖原有成功记录。
  任务状态区分完成、部分完成、失败与中断，错误显示在采集任务页面。
  完成状态按所需数据是否齐全验收，而非请求处理数量；界面列出每场具体缺项和无法计算原因。
  零场完整或本次完全没有更新时标为失败；沿用旧快照不能把失败的补采标成成功。
  同一来源的验证拦截合并提示，浏览器错误保留脱敏后的真实原因。
  只有包含实际日期与比分的战绩表才计为已取得；空壳与仅未来赛程不会计入完整数据。
  来源触发人机验证后，本次任务暂停对该来源的继续请求，不把验证码页解析成空比赛数据。
  未配置可用代理或遇到来源安全拦截时，不能保证自动采集成功；记录实际错误，不伪造成功。
  后台一次仅执行一个独立采集任务，任务超时30分钟，过期任务可重新发起。
  本版未添加定时任务，点击采集才开始；不会自动启动原VIPC任务。

已接入的接口
  体彩接口前缀：https://webapi.sporttery.cn/gateway/uniform/football/
    getVoteV1.qry：HAD/HHAD支持比例、票数、配对奖金、概率、误差和心理误差。
    不再调用体彩getMatchHeadV1、getFixedBonusV1、getMatchResultV1、getResultHistoryV1。
  足彩网POST https://cp.zgzcw.com/ssc/lottery/jcsp/getjcspbymatchId.jsp
    表单cid=wsl或handicap_wsl、sptype=smg、matchId=足彩网CP比赛ID、date=竞彩日期。
    返回真实胜平负/让球初始及近期变化；源站明确第0条是初始，其后为近期变化倒序。
    保留首条初始语义和每条真实时间，历史按时间排序；不声称近期数组包含全部历史。
  足彩网GET https://cp.zgzcw.com/ssc/lottery/getJCBDBFSP.jsp
    lottery=23取得31项比分赔率，lottery=24取得8项总进球赔率，matchId=足彩网CP比赛ID。
    返回数字文本，直接解析接口响应，不解析HTML表格；只作为即时报价，不伪造初盘或更新时间。
    CP比赛ID与分析比赛ID由足彩网竞彩页的tr_id/newplayid显式对应，并验证日期、编号、球队和开赛时间。
  足彩网GET https://cp.zgzcw.com/lottery/jchtplayvsForJsp.action
    参数lotteryId=47、type=jcmini、issue=竞彩日期。每任务读取一次HTML，仅作CP奖金身份关联。
    核对responseJson.issue、CP ID、newplayid、双方球队、编号、让球和“比赛时间”字段。
    页面tr.t是停售时间，不用它代替真实开赛时间；奖金仍由上述4个数据请求取得。
  足彩网POST https://cp.zgzcw.com/ssc/lottery/jcsp/getjcspbydate.jsp
    当前默认只请求模型所需的平均值(0)、威*(9)、36*(2)的europe与kelly，最多6次数据批量请求。
    不请求未使用的bwi*概率、其他公司的CP附加数据或CP返还率；既有原文保留可查。
    已核验的表单类型包括europe/kelly/probability/return，但接口支持范围不等于默认采集范围。
    每任务先请求getjcspcompany.jsp，按返回的公司及数据类型组织请求；目录及数据响应均保留。
    已核验目录含0平均值、7澳*、5立*、9威*、114 bwi*、2 36*，目录读取失败时才使用该清单并记录警告。
    不因公司发布欧赔就推断它也发布凯利或概率；未核验的betting_ratio不当成已取得的支持率。
    返回JSON数组，各项MATCHID是CP比赛ID，SP和SP_CHANGE本身是JSON数组字符串。
    europe.SP为6项：初始胜平负及即时胜平负；SP_CHANGE是带真实时间的近期变化。
    kelly.SP为3项即时凯利；probability.SP为3项概率百分比；return.SP的索引1是返还率，
    两边为空字符串占位。凯利、概率和返还率响应没有各自更新时间，也没有初始凯利。
    按已验证的当日CP ID筛选，返回额外旧比赛不等于整批无效；缺失比赛单独记录。
    只有最新SP_CHANGE的价格与SP即时价完全一致，才把该变化时间标为即时价更新时间。
    不同来源报价不一致时不混配附加字段；原始四种记录与来源公司ID一并保留。
    cid9四种原响应实测覆盖当日11场，证据为source-split/cp-extras-9-*.json；
    公司清单是源站公布的参数范围，各次采集仍须验收实际返回，不能用清单替代成功证据。
  足彩网POST https://plzx.zgzcw.com/odds/oyzs_ajax.action
    表单type=jc、issue=竞彩日期、date为空、companys=公司ID列表。
    按欧亚指数选择器和百家指数亚盘选择器，分2批请求18个公司ID，每批不超过10个。
    额外的Eur*（ID12）实测仅返回公司身份、不含本批赔率，仍保留请求；不会把空值填成报价。
    返回赛程映射，以及欧赔、亚盘、大小球真实初盘和即时盘；不把最近记录当初盘。
  足彩网GET https://fenxi.zgzcw.com/json/{足彩网比赛ID}/bjop
    返回更多公司的最新欧赔、返还率和近期变化记录；不同公司的记录数不固定。
    公司ID与欧亚指数接口的ID空间不同，按已确认的公司标签对应，保留原始ID。
    官方(胜平负)初赔由足彩网CP奖金接口补齐，其余可用初赔先取欧亚指数接口。
  足彩网GET https://fenxi.zgzcw.com/{分析比赛ID}/bsls
    当前已查业务脚本只对历史表赔率发起/bsls/oy请求，日期/球队/比分在HTML三张表中。
    近期战绩及交锋明确标为页面来源；不再使用体彩战绩接口，也不会把/bsls/oy当作比分接口。

仅补充无接口覆盖的字段
  CP批量接口优先提供模型需要的欧赔与即时凯利。
  亚盘/大小球以接口初即盘口和双侧水位为输入，齐备时不再为未使用的凯利、概率、返还率加载页面。
  欧赔页面仅补模型核心公司需要的报价、凯利与配置的完整历史，不为非核心附加数据打开页面。
  欧赔页面还提供四核心公司的完整历史链接；最早凯利仍从真实完整历史取得，
  当前CP的即时凯利接口没有提供最早凯利，不能直接当初始值。
  页面已有报价不得覆盖有效API报价。页面补充失败时保留API数据并列出缺项。
  即时价格或盘口不一致时，不把页面凯利、概率或返还率配到接口价格上；详情显示两组价格和未合并原因。
  已验证的CP完整赔率与凯利可成套作为模型输入，原展示报价仍保留；详情列出模型实际选择的来源及赔率。
  切换整套CP报价前核对比赛映射、公司及报价有效性，初始凯利只在对应赔率匹配选用初赔时带入。
  若完整历史最早记录的赔率、凯利及时间与已保存初始字段一致，且不晚于赛前快照，可整组作为模型初盘输入；明确标注历史初盘来源与时间，即时报价来源不变。
  初始凯利读取完整历史中最早有效时间；JSON最近10条记录不能冒充完整历史。
  最早凯利同时保存该条赔率和时间；与接口初赔不同会明确注明，不改写接口初赔。
  每份快照保存接口URL、GET/POST、表单、响应JSON、实际获取时间和请求方式；
  页面补字段另存原文、字段清单、来源URL及理由。旧快照保留“历史记录”标记。
  比赛列表显示“接口/补字段/历史”的项数，详情可展开查看实际获取方式、请求参数及获取时间。
  没有页面补字段或完整历史需求时，不再无条件加载报价页面。
  必要公司整行缺失也会触发补页：欧赔为平均/威廉/Bet365/平博，亚盘和大小球为威廉/Bet365/澳门；补页仍缺公司时明确报告。补页发现的CP公司只关联本批已核实的接口组合，不增加请求。
  独立SQLite自动增补ps_snapshots.acquisition列，不修改VIPC或MySQL表。
  新分工快照标记sourcePolicy=sporttery_votes_zgzcw_v1。详情、列表和模型共同校验来源及此版本；
  旧分工快照原文保留，但不能填新分工的缺项，也不能将旧体彩初赔混入足彩网欧赔。
  已保存核验样本中，/ssc/json/jcplaysp_49/22/23/24.json返回旧比赛959xx，
  /lottery/jcplaysp.action返回空对象，因此当前流程未采用这些响应；这不代表它们永远无效。
  HTTP200不能代替比赛身份及有效字段验收。

数据与计算口径
  盈亏率=(1-用户投票支持比例×该页配对赔率)×100%，支持比例先转为0—1。
  若按界面上的百分数输入，等价公式为：盈亏率(%)=100-支持率(%)×配对赔率。
  冷热差=用户投票支持率-奖金对应概率，单位为百分点。
  这两个值是本地测算，不标为真实资金成交或VIPC原始盈亏/冷热。
  来源概率缺失或无效时留空，冷热差也留空，不再用赔率反推概率后冒称来源值。
  投票与配对赔率一起计算；奖金详情较新时单列显示，不跨时间静默混合。
  盈亏详情可直接打开参与测算的投票原始快照；让球数取同份投票响应，不借用奖金表的让球。
  最早凯利按来源记录时间排序，不能直接取最新在前的数组第0项。
  本地截止时间采用北京时间开赛时刻，不作为来源官方停售标记。
  实时比分、角球、加时、点球不作为采集必需项；积分榜/联赛统计不阻塞任务。
  缺失值保留为空，不填0。数据详情可查看原始快照、采集时间与来源。

模型结果与筛选
  public-source-model-v2增加澳门/同盘口平均水位参数及实际欧赔输入来源说明。
  盘口参数包含澳门初即盘口、双水位、两侧水差、同盘口平均值及澳门相对均值差，并附公司数。
  均值按同市场、同阶段、同盘口真实公司等权计算，含澳门，排除平均合成行和重复公司。
  源站大小球原值为大球/盘口/小球，详情参数表转换为小球在左、大球在右；原冻结模型方向保持原契约。
  这些新增澳门/均值参数明确标记usedByFrozenModels=false；当前固定模型没有其特征位，后续训练后才能影响评分。
  比赛列表和详情均展示：入选平、次选胜负、二次筛选、两比分、大小球。
  列表可筛选入选平、二次保留、未入选、待计算，并显示各类场数。
  模型输入固定取各类数据最后一份可核对时间的赛前快照。
  竞彩奖金、欧赔、亚盘、大小球、战绩五项赛前快照全部齐备才调用模型。
  只有赛前奖金、其他项目赛后才补齐时，明确显示无法计算，不用缺省特征凑出评分。
  开赛之后新采集的数据仅更新原文/报价展示，不替换模型赛前输入。
  战绩表按日期、球队和完整比分解析；排除本场、同日无明确完成时间的比赛、未来记录及重复记录。
  名称/盘口通过独立转换层进入原模型契约，内部兼容编号不是来源ID，也不代表读过原VIPC表。
  入选平沿用原模型门槛和至少1场有效交锋要求；二次保留沿用真实初即时报价与0.225门槛。
  大小球独立于是否入选平；两比分仅在二次保留之后提供。
  结果缓存在ps_model_results，赛前快照、公司映射或模型程序变化时自动重新计算。
  通过“更新当日赛果”或详情中的“更新本场赛果”独立获取赛果，确认常规时间比分后标注命中；未知比分不当0:0。
  样本日本—巴西的结果是未入选平，大小球为大3球；这不是模型功能缺失。

盈亏与差值
  列表和详情同时展示投票盈亏金额/盈亏率、模型模拟盈亏及初盘/即时盘差值。
  投票盈亏旁直接列出支持率与配对赔率，模型模拟则明确使用平均赔率及估算散户分布。
  盈利金额沿用现有分析的5000万元假设总投注基数：金额=基数×盈亏率/100。
  该基数不是源站真实成交总额；投票测算与模型模拟分开显示。
  初盘及即时盘的体彩差值=威廉赔率-竞彩赔率，均赔差值=威廉赔率-平均欧赔。
  差值和模拟盈亏直接复用原观察页计算/展示组件，不在新页面另写一套差值算法。
  中间盘缺少公司完整变化记录时显示缺失原因，不插值假造中间记录。

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
    python -m unittest discover -s scripts/public_sources -p "test_*.py" -v
    在go_admin目录执行 go test ./handlers ./routes
    在admin目录执行 npm run build

赛果更新与命中核对
  POST /api/admin/public-sources/results 接收 date 和可选 officialId，创建 kind=results 的独立任务；与普通采集共用排他队列。
  从本地已保存比赛及已接受的足彩网比赛头出发，不依赖比赛是否仍在体彩投票列表；不重采赔率、奖金或战绩。
  单次按竞彩日请求足彩网欧亚指数接口 https://plzx.zgzcw.com/odds/oyzs_ajax.action（type=jc，companys=2）。
  赛果严格核对来源比赛ID、主客顺序、竞彩编号、开赛时间；允许竞彩日次日凌晨开赛。
  仅 MATCH_STATUS=-1、双方有效非负整数比分、采集时间不早于开赛时确认常规时间赛果；取消、延期、未知或冲突状态不结算。
  该接口比分排除加时/点球，已用4345436加时赛、4335455点球赛与官方赛报交叉验证；证据在 reports/source-probes/public-results-20261116/result-contract-evidence.json。
  ps_result_snapshots 独立保存结果、比赛绑定、请求原文、时间和哈希；GET /api/admin/public-sources/results/raw 按比赛ID和赛果快照ID查看原文。
  普通失败或不完整响应保留既有确认赛果并提示最近错误；取消/延期与既有完赛冲突则转为待核实；来源明确修订保留历史后更新。
  结果仅在响应中的模型副本附加，不改 ps_snapshots、赛前模型输入或 ps_model_results 缓存；比分不会反向改变入选条件。
  列表按当前日期全部比赛统计：入选平、二次保留、前二比分均只纳入有对应赛前预测且赛果确认的场次；大小球分列全赢/赢半/走水/输半/全输。
  无赛前预测的比赛仍显示确认比分，但不计入命中率分母。页面刷新只读本地结果，更新赛果按钮才会请求来源。

上线/使用
  修改已完成构建，不替换正在运行的服务进程。
  本次新增赛果路由涉及go_admin、Python脚本和前端；由用户自行重启go_admin后，新按钮接口生效。无需重启go_server。
  前端开发服务通常由热更新加载，刷新浏览器即可；静态部署使用新admin/dist。
  首次部署模块或修改go_admin环境变量时，由用户自行重启go_admin。
  静态部署使用新admin/dist；本次构建已生成该目录。
  本次UI核验使用临时本地预览、独立数据库副本与专用测试登录，未修改生产登录或鉴权。
