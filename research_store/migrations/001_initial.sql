CREATE TABLE IF NOT EXISTS schema_migrations (
 version VARCHAR(32) NOT NULL COMMENT '不可变迁移版本',
 checksum CHAR(64) NOT NULL COMMENT '迁移SQL的SHA256',
 applied_at DATETIME(6) NOT NULL COMMENT '实际执行时间UTC',
 PRIMARY KEY (version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='研究库版本迁移记录';

CREATE TABLE IF NOT EXISTS ingest_events (
 id BIGINT NOT NULL AUTO_INCREMENT COMMENT '原始采集事件ID',
 event_hash CHAR(64) NOT NULL COMMENT '来源及接收时点及原文的幂等指纹',
 payload_hash CHAR(64) NOT NULL COMMENT '原始响应SHA256',
 source VARCHAR(64) NOT NULL COMMENT '来源服务',
 kind VARCHAR(64) NOT NULL COMMENT '响应种类',
 match_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '源比赛ID或空值代表批量赛程',
 observed_at DATETIME(6) NOT NULL COMMENT '系统真实收到原文的时间UTC',
 source_at DATETIME(6) NULL COMMENT '来源声明的变动时间UTC未知为NULL',
 provenance VARCHAR(32) NOT NULL COMMENT 'live真实采集或reconstructed旧库重建',
 parser_version VARCHAR(100) NOT NULL COMMENT '采集解析代码版本',
 request_url TEXT NOT NULL COMMENT '去除敏感查询后的来源地址',
 http_status INT NOT NULL COMMENT '实际HTTP状态未知为0',
 payload LONGBLOB NOT NULL COMMENT '未经清洗的原始响应字节允许非JSON及非UTF8',
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT '档案落库时间UTC',
 PRIMARY KEY(id), UNIQUE KEY uq_ingest_event(event_hash),
 KEY ix_ingest_match_kind_time(match_id,kind,observed_at),
 KEY ix_ingest_source_time(source,observed_at), KEY ix_ingest_payload(payload_hash),
 KEY ix_ingest_local_content(source,kind,match_id,payload_hash,provenance,parser_version,observed_at,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='原始响应追加档案禁止覆盖';

CREATE TABLE IF NOT EXISTS entities (
 entity_key VARCHAR(100) NOT NULL COMMENT '标准实体键含类型前缀',
 entity_type VARCHAR(32) NOT NULL COMMENT 'team球队或league联赛',
 name VARCHAR(255) NOT NULL COMMENT '标准展示名称',
 source VARCHAR(64) NOT NULL COMMENT '身份来源',
 external_id VARCHAR(100) NOT NULL COMMENT '来源原始实体ID',
 metadata JSON NOT NULL COMMENT '可追溯映射附加信息',
 updated_at DATETIME(6) NOT NULL COMMENT '标准映射更新时间UTC',
 PRIMARY KEY(entity_key), KEY ix_entity_source(source,entity_type,external_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='标准球队与联赛身份字典';

CREATE TABLE IF NOT EXISTS entity_aliases (
 id BIGINT NOT NULL AUTO_INCREMENT COMMENT '身份别名记录ID',
 entity_key VARCHAR(100) NOT NULL COMMENT '对应标准实体键',
 source VARCHAR(64) NOT NULL COMMENT '外部来源',
 external_id VARCHAR(100) NOT NULL COMMENT '该来源实体ID可为空',
 alias VARCHAR(255) NOT NULL COMMENT '原始名称别名',
 confidence VARCHAR(32) NOT NULL COMMENT 'ID精确或无歧义名称等匹配等级',
 mapping_hash CHAR(64) NOT NULL COMMENT '映射内容幂等指纹',
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT '映射创建时间UTC',
 PRIMARY KEY(id), UNIQUE KEY uq_alias_hash(mapping_hash),
 KEY ix_alias_lookup(source,external_id), KEY ix_alias_entity(entity_key),
 CONSTRAINT fk_alias_entity FOREIGN KEY(entity_key) REFERENCES entities(entity_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='外部身份和名称映射禁止歧义合并';

CREATE TABLE IF NOT EXISTS matches (
 match_id VARCHAR(64) NOT NULL COMMENT '标准比赛ID沿用权威源matchId',
 home_id VARCHAR(100) NOT NULL COMMENT '主队来源ID缺失为空',
 away_id VARCHAR(100) NOT NULL COMMENT '客队来源ID缺失为空',
 home_name VARCHAR(255) NOT NULL COMMENT '主队名称',
 away_name VARCHAR(255) NOT NULL COMMENT '客队名称',
 league_id VARCHAR(100) NOT NULL COMMENT '联赛来源ID缺失为空',
 league_name VARCHAR(255) NOT NULL COMMENT '联赛名称',
 schedule_date DATE NOT NULL COMMENT '源站北京时间赛程日不由UTC日期推导',
 kickoff DATETIME(6) NOT NULL COMMENT '真实开球时间UTC',
 jingcai_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '竞彩编号非竞彩为空',
 source_event_id BIGINT NULL COMMENT '身份来自的原始事件',
 updated_at DATETIME(6) NOT NULL COMMENT '当前实体映射更新时间UTC',
 PRIMARY KEY(match_id), KEY ix_matches_day(schedule_date,kickoff),
 KEY ix_matches_jingcai(jingcai_id,schedule_date),
 KEY ix_matches_home(home_id,kickoff), KEY ix_matches_away(away_id,kickoff),
 CONSTRAINT fk_match_source FOREIGN KEY(source_event_id) REFERENCES ingest_events(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='标准赛事实体当前索引原证据由事件留存';

CREATE TABLE IF NOT EXISTS market_quotes (
 id BIGINT NOT NULL AUTO_INCREMENT COMMENT '报价事件ID',
 quote_hash CHAR(64) NOT NULL COMMENT '报价全部内容的幂等指纹',
 match_id VARCHAR(64) NOT NULL COMMENT '标准比赛ID',
 company VARCHAR(64) NOT NULL COMMENT '明确公司标识',
 market_type VARCHAR(32) NOT NULL COMMENT 'euro亚盘asian或大小球goals等玩法',
 phase VARCHAR(32) NOT NULL COMMENT 'opening初盘或latest即时或tick变动',
 source_event_id BIGINT NULL COMMENT '所属原始响应事件',
 source_at DATETIME(6) NULL COMMENT '报价来源时点未知为NULL',
 observed_at DATETIME(6) NOT NULL COMMENT '系统实际获得报价时间UTC',
 provenance VARCHAR(32) NOT NULL COMMENT '真实采集或历史重建',
 home_value DECIMAL(22,10) NULL COMMENT '主胜赔率或明确主方向数值缺失为NULL',
 draw_value DECIMAL(22,10) NULL COMMENT '平局赔率缺失为NULL',
 away_value DECIMAL(22,10) NULL COMMENT '客胜赔率或明确客方向数值缺失为NULL',
 line_value DECIMAL(22,10) NULL COMMENT '让球或大小球盘口真实0可保存',
 home_water DECIMAL(22,10) NULL COMMENT '主队或大球水位缺失为NULL',
 away_water DECIMAL(22,10) NULL COMMENT '客队或小球水位缺失为NULL',
 raw_json JSON NOT NULL COMMENT '来源原始盘口单行',
 PRIMARY KEY(id), UNIQUE KEY uq_quote_hash(quote_hash),
 KEY ix_quote_asof(match_id,company,market_type,observed_at),
 KEY ix_quote_source(source_event_id),
 CONSTRAINT fk_quote_match FOREIGN KEY(match_id) REFERENCES matches(match_id),
 CONSTRAINT fk_quote_source FOREIGN KEY(source_event_id) REFERENCES ingest_events(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='公司玩法阶段分离的不可变报价事件';

CREATE TABLE IF NOT EXISTS match_results (
 id BIGINT NOT NULL AUTO_INCREMENT COMMENT '赛果修订ID',
 result_hash CHAR(64) NOT NULL COMMENT '赛果版本的幂等指纹',
 match_id VARCHAR(64) NOT NULL COMMENT '标准比赛ID',
 home_score INT NULL COMMENT '常规90分钟主队进球未知为NULL',
 away_score INT NULL COMMENT '常规90分钟客队进球未知为NULL',
 extra_home INT NULL COMMENT '加时主队进球未知为NULL',
 extra_away INT NULL COMMENT '加时客队进球未知为NULL',
 penalty_home INT NULL COMMENT '点球主队进球未知为NULL',
 penalty_away INT NULL COMMENT '点球客队进球未知为NULL',
 regulation_confirmed BOOLEAN NOT NULL COMMENT '是否能确认90分钟赛果',
 status VARCHAR(64) NOT NULL COMMENT '来源完赛或歧义状态',
 available_at DATETIME(6) NULL COMMENT '赛果首次已知时间未知为NULL',
 observed_at DATETIME(6) NOT NULL COMMENT '实际获得该赛果版本时间UTC',
 source_event_id BIGINT NULL COMMENT '原始赛果来源事件',
 provenance VARCHAR(32) NOT NULL COMMENT '真实记录或历史重建',
 PRIMARY KEY(id), UNIQUE KEY uq_result_hash(result_hash),
 KEY ix_result_match(match_id,observed_at),
 CONSTRAINT fk_result_match FOREIGN KEY(match_id) REFERENCES matches(match_id),
 CONSTRAINT fk_result_source FOREIGN KEY(source_event_id) REFERENCES ingest_events(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='常规加时点球分离的赛果追加版本';

CREATE TABLE IF NOT EXISTS feature_definitions (
 id BIGINT NOT NULL AUTO_INCREMENT COMMENT '特征定义ID',
 feature_version VARCHAR(100) NOT NULL COMMENT '整套特征定义版本',
 feature_name VARCHAR(190) NOT NULL COMMENT '稳定唯一特征名称',
 value_type VARCHAR(32) NOT NULL COMMENT '数值或布尔等类型',
 unit VARCHAR(64) NOT NULL COMMENT '单位或比例口径',
 description TEXT NOT NULL COMMENT '特征业务解释',
 formula TEXT NOT NULL COMMENT '具体计算依据或表达式',
 origin VARCHAR(64) NOT NULL COMMENT '原始真实或本地推算模拟来源类别',
 missing_policy TEXT NOT NULL COMMENT '缺失处理及样本门槛',
 code_hash VARCHAR(100) NOT NULL COMMENT '计算代码或模型定义哈希',
 metadata JSON NOT NULL COMMENT '依赖字段和有效性门槛等完整元数据',
 definition_hash CHAR(64) NOT NULL COMMENT '定义不可变内容指纹',
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT '注册时间UTC',
 PRIMARY KEY(id), UNIQUE KEY uq_feature_version_name(feature_version,feature_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='公式单位依赖及缺失策略版本字典';

CREATE TABLE IF NOT EXISTS feature_snapshots (
 id BIGINT NOT NULL AUTO_INCREMENT COMMENT '不可变特征快照ID',
 snapshot_hash CHAR(64) NOT NULL COMMENT '输入版本与质量内容指纹不含重复采集时间',
 match_id VARCHAR(64) NOT NULL COMMENT '目标比赛ID',
 as_of DATETIME(6) NOT NULL COMMENT '这份输入声称的预测截止时间UTC',
 kickoff DATETIME(6) NOT NULL COMMENT '封存时目标开球时间UTC',
 feature_version VARCHAR(100) NOT NULL COMMENT '所用特征公式版本',
 data_revision VARCHAR(100) NOT NULL COMMENT '原始数据批次或重建版本',
 provenance VARCHAR(32) NOT NULL COMMENT 'live真实赛前或reconstructed历史重建',
 known_by DATETIME(6) NULL COMMENT '全部依赖真实已知时间上界未知为NULL',
 quality VARCHAR(64) NOT NULL COMMENT '样本质量等级',
 values_json JSON NOT NULL COMMENT '仅预测输入不含本场赛果',
 missing_json JSON NOT NULL COMMENT '原始缺失字段及位图保留插补前状态',
 quality_flags JSON NOT NULL COMMENT '时间身份缺失取样等审计标记',
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT '实际封存时间UTC',
 PRIMARY KEY(id), UNIQUE KEY uq_snapshot_hash(snapshot_hash),
 UNIQUE KEY uq_snapshot_revision(match_id,feature_version,data_revision),
 KEY ix_snapshot_scope(feature_version,provenance,as_of),
 KEY ix_snapshot_match(match_id,as_of),
 CONSTRAINT fk_snapshot_match FOREIGN KEY(match_id) REFERENCES matches(match_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='截止时点特征原始缺失及质量的不可变快照';

CREATE TABLE IF NOT EXISTS snapshot_sources (
 snapshot_id BIGINT NOT NULL COMMENT '特征快照ID',
 event_id BIGINT NOT NULL COMMENT '实际依赖的原始事件ID',
 PRIMARY KEY(snapshot_id,event_id),
 CONSTRAINT fk_ss_snapshot FOREIGN KEY(snapshot_id) REFERENCES feature_snapshots(id),
 CONSTRAINT fk_ss_event FOREIGN KEY(event_id) REFERENCES ingest_events(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='每份特征的原始数据血缘';

CREATE TABLE IF NOT EXISTS predictions (
 id BIGINT NOT NULL AUTO_INCREMENT COMMENT '不可变预测ID',
 prediction_hash CHAR(64) NOT NULL COMMENT '预测内容指纹',
 snapshot_id BIGINT NOT NULL COMMENT '输入快照ID',
 match_id VARCHAR(64) NOT NULL COMMENT '目标比赛ID',
 model_version VARCHAR(100) NOT NULL COMMENT '冻结模型版本',
 rule_version VARCHAR(100) NOT NULL COMMENT '二选及增强规则版本',
 predicted_at DATETIME(6) NOT NULL COMMENT '实际生成预测时间UTC',
 state VARCHAR(64) NOT NULL COMMENT '原模型入选状态',
 score DOUBLE NULL COMMENT '原模型分数并非已校准概率',
 threshold_value DOUBLE NULL COMMENT '原模型固定门槛',
 secondary_selected BOOLEAN NOT NULL COMMENT '二次筛选是否保留',
 enhanced BOOLEAN NOT NULL COMMENT '是否盈亏分歧增强',
 score_method VARCHAR(100) NOT NULL COMMENT '双比分计算版本',
 scorelines JSON NOT NULL COMMENT '两个推荐比分及说明不含本场赛果',
 payload JSON NOT NULL COMMENT '完整模型决定及推断说明',
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT '预测写入时间UTC',
 PRIMARY KEY(id), UNIQUE KEY uq_prediction_revision(snapshot_id,model_version,rule_version,score_method),
 KEY ix_prediction_match(match_id,predicted_at), KEY ix_prediction_scope(model_version,secondary_selected,predicted_at),
 CONSTRAINT fk_prediction_snapshot FOREIGN KEY(snapshot_id) REFERENCES feature_snapshots(id),
 CONSTRAINT fk_prediction_match FOREIGN KEY(match_id) REFERENCES matches(match_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='原二次增强与双比分决定的封存记录';

CREATE TABLE IF NOT EXISTS settlements (
 id BIGINT NOT NULL AUTO_INCREMENT COMMENT '结算记录ID',
 prediction_id BIGINT NOT NULL COMMENT '原预测ID',
 result_id BIGINT NOT NULL COMMENT '本次采用的真实赛果版本',
 is_draw BOOLEAN NOT NULL COMMENT '是否常规平局',
 scoreline_hit BOOLEAN NOT NULL COMMENT '是否命中两个推荐比分之一',
 settled_at DATETIME(6) NOT NULL COMMENT '实际结算时间UTC',
 PRIMARY KEY(id), UNIQUE KEY uq_settlement_revision(prediction_id,result_id),
 CONSTRAINT fk_settlement_prediction FOREIGN KEY(prediction_id) REFERENCES predictions(id),
 CONSTRAINT fk_settlement_result FOREIGN KEY(result_id) REFERENCES match_results(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='赛后追加结算不改写原预测';

CREATE TABLE IF NOT EXISTS audit_runs (
 id BIGINT NOT NULL AUTO_INCREMENT COMMENT '清洗审计批次ID',
 kind VARCHAR(64) NOT NULL COMMENT '审计任务种类',
 version VARCHAR(100) NOT NULL COMMENT '审计规则版本',
 started_at DATETIME(6) NOT NULL COMMENT '开始时间UTC',
 finished_at DATETIME(6) NULL COMMENT '完成时间UTC',
 status VARCHAR(32) NOT NULL COMMENT 'running或completed或failed',
 metadata JSON NOT NULL COMMENT '范围源版本与运行结果摘要',
 PRIMARY KEY(id), KEY ix_audit_run_time(started_at,status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='每次清洗审计运行登记';

CREATE TABLE IF NOT EXISTS audit_issues (
 id BIGINT NOT NULL AUTO_INCREMENT COMMENT '质量问题ID',
 issue_hash CHAR(64) NOT NULL COMMENT '同批次问题幂等指纹',
 run_id BIGINT NOT NULL COMMENT '所属审计批次',
 match_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '相关比赛ID空为全局问题',
 source_event_id BIGINT NULL COMMENT '相关原始事件ID',
 severity VARCHAR(32) NOT NULL COMMENT 'info或warning或error',
 code VARCHAR(100) NOT NULL COMMENT '稳定质量问题代码',
 field_name VARCHAR(190) NOT NULL COMMENT '问题字段或输入组',
 message TEXT NOT NULL COMMENT '可读问题说明',
 detail JSON NOT NULL COMMENT '原值修正值及处理依据',
 status VARCHAR(32) NOT NULL DEFAULT 'open' COMMENT '待处理或已标记解决',
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) COMMENT '问题记录时间UTC',
 PRIMARY KEY(id), UNIQUE KEY uq_audit_issue(issue_hash),
 KEY ix_audit_issue_batch(run_id,severity,status), KEY ix_audit_issue_match(match_id,status),
 CONSTRAINT fk_issue_run FOREIGN KEY(run_id) REFERENCES audit_runs(id),
 CONSTRAINT fk_issue_source FOREIGN KEY(source_event_id) REFERENCES ingest_events(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='异常隔离与原因审计清单';

CREATE TABLE IF NOT EXISTS datasets (
 id BIGINT NOT NULL AUTO_INCREMENT COMMENT '固定数据集ID',
 name VARCHAR(100) NOT NULL COMMENT '数据集用途名称',
 version VARCHAR(100) NOT NULL COMMENT '不可变数据集版本',
 feature_version VARCHAR(100) NOT NULL COMMENT '使用的特征版本',
 manifest_hash CHAR(64) NOT NULL COMMENT '数据清单内容哈希',
 manifest JSON NOT NULL COMMENT '选择口径时间切分及排除原因',
 created_at DATETIME(6) NOT NULL COMMENT '创建时间UTC',
 PRIMARY KEY(id), UNIQUE KEY uq_dataset_version(name,version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='训练校准测试数据集的固定版本';

CREATE TABLE IF NOT EXISTS dataset_members (
 dataset_id BIGINT NOT NULL COMMENT '所属固定数据集',
 snapshot_id BIGINT NOT NULL COMMENT '具体不可变输入快照',
 result_id BIGINT NULL COMMENT '标签赛果版本无标签为NULL',
 split_name VARCHAR(32) NOT NULL COMMENT 'train或calibration或test或research或prospective赛前无标签',
 PRIMARY KEY(dataset_id,snapshot_id), KEY ix_dataset_split(dataset_id,split_name),
 CONSTRAINT fk_member_dataset FOREIGN KEY(dataset_id) REFERENCES datasets(id),
 CONSTRAINT fk_member_snapshot FOREIGN KEY(snapshot_id) REFERENCES feature_snapshots(id),
 CONSTRAINT fk_member_result FOREIGN KEY(result_id) REFERENCES match_results(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='数据集固定比赛和输入标签成员';

CREATE TABLE IF NOT EXISTS model_runs (
 id BIGINT NOT NULL AUTO_INCREMENT COMMENT '训练或注册模型运行ID',
 version VARCHAR(100) NOT NULL COMMENT '模型工件版本',
 dataset_id BIGINT NOT NULL COMMENT '采用的数据集版本',
 artifact_hash VARCHAR(100) NOT NULL COMMENT '模型工件SHA256',
 artifact_uri TEXT NOT NULL COMMENT '工件文件位置不存大文件到Git',
 config JSON NOT NULL COMMENT '训练配置及特征版本',
 status VARCHAR(32) NOT NULL COMMENT 'candidate或rejected或registered',
 started_at DATETIME(6) NOT NULL COMMENT '训练或登记时间UTC',
 PRIMARY KEY(id), UNIQUE KEY uq_model_run(version,dataset_id,artifact_hash),
 CONSTRAINT fk_model_dataset FOREIGN KEY(dataset_id) REFERENCES datasets(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='候选与既有模型的版本工件登记';

CREATE TABLE IF NOT EXISTS evaluations (
 id BIGINT NOT NULL AUTO_INCREMENT COMMENT '评估结果ID',
 evaluation_hash CHAR(64) NOT NULL COMMENT '评估内容幂等指纹',
 model_run_id BIGINT NULL COMMENT '对应模型运行ID',
 dataset_id BIGINT NULL COMMENT '评估使用的固定数据集ID',
 version VARCHAR(100) NOT NULL COMMENT '评估逻辑版本',
 scope VARCHAR(100) NOT NULL COMMENT '原模型二选增强比分等范围',
 metrics JSON NOT NULL COMMENT '分母明确的命中覆盖样本数等指标',
 created_at DATETIME(6) NOT NULL COMMENT '实际评估时间UTC',
 PRIMARY KEY(id), UNIQUE KEY uq_evaluation_hash(evaluation_hash),
 KEY ix_evaluation_scope(scope,created_at),
 CONSTRAINT fk_evaluation_model FOREIGN KEY(model_run_id) REFERENCES model_runs(id),
 CONSTRAINT fk_evaluation_dataset FOREIGN KEY(dataset_id) REFERENCES datasets(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='各版本各人群的可复现效果记录';
