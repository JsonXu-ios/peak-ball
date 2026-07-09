-- football_data seed.sql
-- 与 GORM 模型完全匹配的演示种子数据
-- GORM 默认 snake_case 列名映射

-- ===================== 用户表 users =====================
-- 字段: id, username, nickname, avatar, email, badge, balance, joined_at, country, created_at, updated_at
INSERT INTO users (id, username, nickname, avatar, email, badge, balance, joined_at, country, created_at, updated_at) VALUES
(1, 'alex_thompson', 'Alex Thompson', '/images/avatar_alex.jpg', 'alex@example.com', 'Pro Predictor', 2450.85, '2023-09-01', '🇬🇧', NOW(), NOW()),
(2, 'marco_silva', 'Marco Silva', '/images/expert_marco.jpg', 'marco@example.com', 'Expert', 5200.00, '2022-06-15', '🇵🇹', NOW(), NOW()),
(3, 'elena_rossi', 'Elena Rossi', '/images/expert_elena.jpg', 'elena@example.com', 'Expert', 3800.00, '2023-01-20', '🇮🇹', NOW(), NOW());

-- ===================== 关注球队表 followed_teams =====================
-- 字段: id, user_id, team_name, team_logo, team_id, created_at
INSERT INTO followed_teams (id, user_id, team_name, team_logo, team_id, created_at) VALUES
(1, 1, 'Man City', '/images/team_mancity.jpg', 50, NOW()),
(2, 1, 'Real Madrid', '/images/team_realmadrid.jpg', 541, NOW()),
(3, 1, 'Barcelona', '/images/team_barcelona.jpg', 529, NOW());

-- ===================== 专家表 experts =====================
-- 字段: id, user_id, name, avatar, specialty, accuracy, streak, followers, verified, created_at, updated_at
INSERT INTO experts (id, user_id, name, avatar, specialty, accuracy, streak, followers, verified, created_at, updated_at) VALUES
(1, 2, 'Marco Silva', '/images/expert_marco.jpg', 'Premier League & Champions League', 78.40, 5, 12400, 1, NOW(), NOW()),
(2, 3, 'Elena Rossi', '/images/expert_elena.jpg', 'Serie A & La Liga', 76.10, 3, 8900, 1, NOW(), NOW());

-- ===================== 新闻表 news =====================
-- 字段: id, title, summary, content, image_url, category, source, club, is_hot, created_at, updated_at
INSERT INTO news (id, title, summary, content, image_url, category, source, club, is_hot, created_at, updated_at) VALUES
(1, 'Man United stage dramatic comeback victory', 'Red Devils fight back from 1-0 down to win 2-1 at Old Trafford.', 'Manchester United produced a stunning second-half comeback at Old Trafford. Trailing 1-0 at the break, goals from Marcus Rashford and Bruno Fernandes sealed a crucial 2-1 victory.', '/images/news_featured1.jpg', 'latest', 'BBC Sport', 'Manchester United', 1, NOW(), NOW()),
(2, 'Messi breaks all-time goal record', 'Lionel Messi scores his 800th career goal in sensational fashion.', 'Lionel Messi reached yet another extraordinary milestone, netting his 800th career goal with a trademark left-footed curler from outside the box.', '/images/news_hero.jpg', 'latest', 'ESPN', 'Inter Miami', 1, NOW(), NOW()),
(3, 'Haaland transfer saga continues', 'Multiple clubs monitoring Erling Haaland as contract talks stall.', 'Erling Haaland could be on the move as negotiations over a new deal with Manchester City have reportedly reached an impasse. Real Madrid, Barcelona and PSG are all monitoring.', '/images/news_soccer.jpg', 'transfer', 'Sky Sports', 'Manchester City', 0, NOW(), NOW()),
(4, 'Premier League introduces new VAR protocol', 'Major changes to the Video Assistant Referee system announced.', 'The Premier League has confirmed sweeping changes to how VAR decisions will be communicated to fans, including live audio from referee conversations.', '/images/news_official.jpg', 'official', 'The Guardian', NULL, 0, NOW(), NOW()),
(5, 'Tactical Analysis: How Arsenal dominate possession', 'An in-depth look at Arteta style of play.', 'Mikel Arteta has transformed Arsenal into one of the most possession-dominant teams in Europe, with an average of 67% ball retention this season.', '/images/news_stadium.jpg', 'analysis', 'The Athletic', 'Arsenal', 0, NOW(), NOW());

-- ===================== 转会传闻表 transfer_rumors =====================
-- 字段: id, player_name, from_club, to_club, from_club_logo, to_club_logo, value, trust_level, tier, status, created_at, updated_at
INSERT INTO transfer_rumors (id, player_name, from_club, to_club, from_club_logo, to_club_logo, value, trust_level, tier, status, created_at, updated_at) VALUES
(1, 'Erling Haaland', 'Manchester City', 'Real Madrid', '/images/team_mancity.jpg', '/images/team_realmadrid.jpg', '€180.0M', 45, 'Tier 2', 'rumor', NOW(), NOW()),
(2, 'Khvicha Kvaratskhelia', 'Napoli', 'PSG', '/images/team_intermilan.jpg', '/images/team_away.jpg', '€85.0M', 72, 'Tier 1', 'confirmed', NOW(), NOW()),
(3, 'Florian Wirtz', 'Bayer Leverkusen', 'Bayern Munich', '/images/team_away2.jpg', '/images/team_away3.jpg', '€120.0M', 60, 'Tier 2', 'rumor', NOW(), NOW());

-- ===================== 通知表 notifications =====================
-- 字段: id, user_id, type, title, message, icon, match_id, is_read, read_at, created_at, updated_at
INSERT INTO notifications (id, user_id, type, title, message, icon, match_id, is_read, read_at, created_at, updated_at) VALUES
(1, 1, 'goal', 'GOAL! Man City 1-0 Arsenal', 'Erling Haaland scores in the 23rd minute with a powerful header.', 'sports_soccer', '498050369', 0, NULL, NOW(), NOW()),
(2, 1, 'red_card', 'Red Card - Arsenal vs Man City', 'Gabriel receives a straight red card for denying a clear goal-scoring opportunity.', 'style', '498050369', 0, NULL, NOW(), NOW()),
(3, 1, 'expert_tip', 'New Expert Tip Available', 'Marco Silva just published a new prediction for tonight match.', 'lightbulb', NULL, 1, NOW(), NOW(), NOW()),
(4, 1, 'reward', 'Daily Reward Claimed', 'You earned 50 points from your daily login streak!', 'redeem', NULL, 1, NOW(), NOW(), NOW()),
(5, 1, 'system', 'System Update', 'We have improved our live match tracking. Enjoy faster score updates!', 'campaign', NULL, 0, NULL, NOW(), NOW());

-- ===================== 钱包交易表 wallet_transactions =====================
-- 字段: id, user_id, type, amount, description, detail, created_at
INSERT INTO wallet_transactions (id, user_id, type, amount, description, detail, created_at) VALUES
(1, 1, 'earned', 500, 'Sign-up Bonus', 'Welcome reward for new users', '2025-12-01 10:00:00'),
(2, 1, 'earned', 50, 'Daily Login Streak', '7-day login reward', '2025-12-08 08:30:00'),
(3, 1, 'spent', -200, 'Expert Tip Unlock', 'Marco Silva - Man City vs Arsenal', '2025-12-10 14:20:00'),
(4, 1, 'earned', 150, 'Prediction Win', 'Correct prediction: Man United win', '2025-12-15 21:00:00'),
(5, 1, 'topup', 1000, 'Top-up via PayPal', 'PayPal transaction #TXN8823', '2025-12-20 11:00:00'),
(6, 1, 'redeem', -50, 'Store Redemption', 'Redeemed for Match Day Badge NFT', '2025-12-25 16:00:00');

-- ===================== 奖励表 rewards =====================
-- 字段: id, name, description, icon, icon_color, cost, is_active, created_at, updated_at
INSERT INTO rewards (id, name, description, icon, icon_color, cost, is_active, created_at, updated_at) VALUES
(1, 'Match Day Badge', 'Exclusive digital badge for match day fans', 'military_tech', 'text-amber-500', 50, 1, NOW(), NOW()),
(2, 'Expert Tip Free Unlock', 'Unlock one expert tip for free', 'lightbulb', 'text-primary', 200, 1, NOW(), NOW()),
(3, 'VIP Seat Raffle Entry', 'Enter the raffle for VIP stadium seats', 'stadium', 'text-emerald-500', 500, 1, NOW(), NOW()),
(4, 'Custom Avatar Frame', 'Show off with a premium avatar border', 'frame_person', 'text-purple-500', 100, 1, NOW(), NOW());

-- ===================== 联赛积分榜 league_standings =====================
-- 字段: id, league_id, league, season, team_id, team_name, team_logo, rank, played, won, drawn, lost, goals_for, goals_ag, goal_diff, points, form, zone, created_at, updated_at
INSERT INTO league_standings (id, league_id, league, season, team_id, team_name, team_logo, `rank`, played, won, drawn, lost, goals_for, goals_ag, goal_diff, points, form, zone, created_at, updated_at) VALUES
(1,  39, 'Premier League', '2025-2026', 50,  'Manchester City',    '/images/team_mancity.jpg',     1, 25, 18, 4, 3, 62, 21, 41, 58, 'WWDWW', 'champion', NOW(), NOW()),
(2,  39, 'Premier League', '2025-2026', 42,  'Arsenal',            '/images/team_arsenal.jpg',     2, 25, 17, 5, 3, 55, 19, 36, 56, 'WDWWW', 'champion', NOW(), NOW()),
(3,  39, 'Premier League', '2025-2026', 40,  'Liverpool',          '/images/team_liverpool.jpg',   3, 25, 16, 5, 4, 58, 25, 33, 53, 'WWLWW', 'champion', NOW(), NOW()),
(4,  39, 'Premier League', '2025-2026', 33,  'Manchester United',  '/images/team_manutd.jpg',      4, 25, 15, 5, 5, 50, 28, 22, 50, 'WLWWW', 'europa',   NOW(), NOW()),
(5,  39, 'Premier League', '2025-2026', 49,  'Chelsea',            '/images/team_chelsea.jpg',     5, 25, 14, 4, 7, 48, 30, 18, 46, 'DWWLW', 'europa',   NOW(), NOW()),
(6,  39, 'Premier League', '2025-2026', 47,  'Tottenham',          '/images/team_tottenham.jpg',   6, 25, 13, 5, 7, 45, 32, 13, 44, 'WDLWW', 'conference', NOW(), NOW()),
(7,  39, 'Premier League', '2025-2026', 66,  'Aston Villa',        '/images/team_astonvilla.jpg',  7, 25, 12, 6, 7, 40, 29, 11, 42, 'LDWWW', NULL,       NOW(), NOW()),
(8,  39, 'Premier League', '2025-2026', 34,  'Newcastle',          '/images/team_newcastle.jpg',   8, 25, 11, 8, 6, 42, 28, 14, 41, 'DWWDL', NULL,       NOW(), NOW()),
(9,  39, 'Premier League', '2025-2026', 52,  'Brighton',           '/images/team_brighton.jpg',    9, 25, 10, 7, 8, 38, 33, 5,  37, 'WLDWW', NULL,       NOW(), NOW()),
(10, 39, 'Premier League', '2025-2026', 48,  'West Ham',           '/images/team_westham.jpg',    10, 25,  9, 6, 10, 32, 38, -6, 33, 'LLWDW', NULL,       NOW(), NOW()),
(11, 39, 'Premier League', '2025-2026', 45,  'Everton',            '/images/team_everton.jpg',    18, 25,  5, 7, 13, 22, 40, -18, 22, 'LDLLL', 'relegation', NOW(), NOW()),
(12, 39, 'Premier League', '2025-2026', 63,  'Fulham',             '/images/team_fulham.jpg',     19, 25,  4, 8, 13, 20, 42, -22, 20, 'LLDLL', 'relegation', NOW(), NOW()),
(13, 39, 'Premier League', '2025-2026', 62,  'Burnley',            '/images/team_burnley.jpg',    20, 25,  3, 5, 17, 15, 50, -35, 14, 'LLLLD', 'relegation', NOW(), NOW());

-- ===================== 射手榜 top_scorers =====================
-- 字段: id, league_id, league, season, player_name, team_name, avatar, goals, assists, rank, created_at, updated_at
INSERT INTO top_scorers (id, league_id, league, season, player_name, team_name, avatar, goals, assists, `rank`, created_at, updated_at) VALUES
(1,  39, 'Premier League', '2025-2026', 'Erling Haaland',   'Manchester City',   '/images/player_haaland.jpg',  24, 5,  1, NOW(), NOW()),
(2,  39, 'Premier League', '2025-2026', 'Mohamed Salah',    'Liverpool',         '/images/player_salah.jpg',    19, 12, 2, NOW(), NOW()),
(3,  39, 'Premier League', '2025-2026', 'Alexander Isak',   'Newcastle',         '/images/player_haaland.jpg',  16, 4,  3, NOW(), NOW()),
(4,  39, 'Premier League', '2025-2026', 'Bukayo Saka',      'Arsenal',           '/images/player_salah.jpg',    14, 10, 4, NOW(), NOW()),
(5,  39, 'Premier League', '2025-2026', 'Marcus Rashford',  'Manchester United', '/images/player_son.jpg',      13, 6,  5, NOW(), NOW()),
(6,  39, 'Premier League', '2025-2026', 'Cole Palmer',      'Chelsea',           '/images/player_haaland.jpg',  12, 8,  6, NOW(), NOW()),
(7,  39, 'Premier League', '2025-2026', 'Ollie Watkins',    'Aston Villa',       '/images/player_salah.jpg',    11, 7,  7, NOW(), NOW()),
(8,  39, 'Premier League', '2025-2026', 'Son Heung-min',    'Tottenham',         '/images/player_son.jpg',      10, 5,  8, NOW(), NOW());

-- ===================== 搜索历史 search_histories =====================
-- 字段: id, user_id, query, created_at
INSERT INTO search_histories (id, user_id, `query`, created_at) VALUES
(1, 1, 'Manchester United', NOW()),
(2, 1, 'Haaland', NOW()),
(3, 1, 'Champions League', NOW());

-- ===================== 排行榜 leaderboard_entries =====================
-- 字段: id, user_id, period, period_key, points, accuracy, rank, trend, created_at, updated_at
INSERT INTO leaderboard_entries (id, user_id, period, period_key, points, accuracy, `rank`, trend, created_at, updated_at) VALUES
(1, 1, 'weekly', '2026-W06', 1420, 72.40, 1, 'up',   NOW(), NOW()),
(2, 2, 'weekly', '2026-W06', 1380, 78.40, 2, 'none', NOW(), NOW()),
(3, 3, 'weekly', '2026-W06', 1250, 76.10, 3, 'up',   NOW(), NOW()),
(4, 1, 'monthly', '2026-02', 5200, 72.40, 1, 'up',   NOW(), NOW()),
(5, 2, 'monthly', '2026-02', 4980, 78.40, 2, 'down', NOW(), NOW()),
(6, 3, 'monthly', '2026-02', 4600, 76.10, 3, 'up',   NOW(), NOW());

-- ===================== 预测 predictions =====================
-- 字段: id, user_id, match_id, pick, odds, stake, profit, status, settled_at, created_at, updated_at
INSERT INTO predictions (id, user_id, match_id, pick, odds, stake, profit, status, settled_at, created_at, updated_at) VALUES
(1, 1, '498050369', 'home', 1.85, 100.00, 85.00,  'won',     '2026-02-10 22:00:00', '2026-02-10 18:00:00', '2026-02-10 22:00:00'),
(2, 1, '498050371', 'over', 1.72, 50.00,  -50.00, 'lost',    '2026-02-10 22:00:00', '2026-02-10 18:30:00', '2026-02-10 22:00:00'),
(3, 1, '498050372', 'away', 2.10, 80.00,  0.00,   'ongoing', NULL,                  '2026-02-11 12:00:00', '2026-02-11 12:00:00');

