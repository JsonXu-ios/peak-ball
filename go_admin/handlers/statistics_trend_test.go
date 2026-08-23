package handlers

import "testing"

func trendRow(id, date, mtime string, homeID, guestID int64, hs, gs, hh, gh int, settled bool) map[string]interface{} {
	display := "完赛"
	if !settled {
		display = "未开始"
	}
	return map[string]interface{}{
		"match_id": id, "date": date, "match_time": mtime,
		"home": "H" + id, "guest": "G" + id,
		"home_team_id": homeID, "guest_team_id": guestID,
		"home_score": hs, "guest_score": gs, "home_half_score": hh, "guest_half_score": gh,
		"status": -1, "display_state": display, "league": "L",
	}
}

func TestTrendRecordPerspective(t *testing.T) {
	match := parseTrendMatch(trendRow("1", "2026-08-01", "2026-08-01 20:00", 10, 20, 3, 1, 1, 0, true))
	home, ok := trendRecordFor(match, 10)
	if !ok || home.Scored != 3 || home.Conceded != 1 || home.HalfScored != 1 || home.HalfConceded != 0 || !home.IsHome || home.Result != "胜" || home.Opponent != "G1" {
		t.Fatalf("home perspective wrong: %+v", home)
	}
	guest, ok := trendRecordFor(match, 20)
	if !ok || guest.Scored != 1 || guest.Conceded != 3 || guest.HalfScored != 0 || guest.HalfConceded != 1 || guest.IsHome || guest.Result != "负" || guest.Opponent != "H1" {
		t.Fatalf("guest perspective wrong: %+v", guest)
	}
	if _, ok := trendRecordFor(match, 99); ok {
		t.Fatal("unrelated team must not match")
	}
}

func TestTrendLastNCutoffAndOrder(t *testing.T) {
	var all []trendMatch
	// team 10 plays 7 settled games; the 7th is the same day as the target but later.
	dates := []string{"2026-07-01", "2026-07-05", "2026-07-03", "2026-07-10", "2026-07-12", "2026-07-15", "2026-07-20"}
	for i, d := range dates {
		mtime := d + " 18:00"
		if i == 6 {
			mtime = d + " 22:00"
		}
		all = append(all, parseTrendMatch(trendRow(string(rune('a'+i)), d, mtime, 10, int64(30+i), i, 1, 0, 0, true)))
	}
	// an unsettled game must never enter the index
	all = append(all, parseTrendMatch(trendRow("x", "2026-07-08", "2026-07-08 18:00", 10, 77, 9, 9, 0, 0, false)))
	index := trendIndex(all)

	team := trendBuildTeam(10, "T", "", nil, index, trendSortKey("2026-07-20", "2026-07-20 20:00"), 5)
	if team.Source != "self" || team.Sample != 5 {
		t.Fatalf("expected 5 self records, got %+v", team)
	}
	want := []string{"2026-07-03", "2026-07-05", "2026-07-10", "2026-07-12", "2026-07-15"}
	for i, rec := range team.List {
		if rec.Date != want[i] {
			t.Fatalf("order/cutoff wrong at %d: %s want %s", i, rec.Date, want[i])
		}
		if rec.Scored == 9 {
			t.Fatal("unsettled game leaked into trend")
		}
	}
	if team.Summary.AttackTrend != "上升" {
		t.Fatalf("scored 1,2,3,4,5 should be 上升, got %s", team.Summary.AttackTrend)
	}
}

func TestTrendHistorySourcePreferred(t *testing.T) {
	history := `[{"matchTime":"2026-06-01 19:00","home":"A","guest":"B","homeId":10,"guestId":11,"goal":[0,2],"halfGoal":[0,1]},
	             {"matchTime":"2026-06-08 19:00","home":"C","guest":"A","homeId":12,"guestId":10,"goal":[1,1],"halfGoal":[1,0]},
	             {"matchTime":"2026-09-01 19:00","home":"A","guest":"B","homeId":10,"guestId":11,"goal":[5,0]}]`
	team := trendBuildTeam(10, "A", "", history, map[int64][]trendRecord{}, trendSortKey("2026-08-01", ""), 5)
	if team.Source != "history" || team.Sample != 2 {
		t.Fatalf("expected 2 history records (future one cut), got %+v", team)
	}
	first, second := team.List[0], team.List[1]
	if first.Scored != 0 || first.Conceded != 2 || first.HalfConceded != 1 || first.Result != "负" {
		t.Fatalf("history home perspective wrong: %+v", first)
	}
	if second.Scored != 1 || second.Conceded != 1 || second.HalfScored != 0 || second.HalfConceded != 1 || second.IsHome {
		t.Fatalf("history guest perspective wrong: %+v", second)
	}
	if team.Summary.AttackTrend != "样本不足" {
		t.Fatalf("2 samples should be 样本不足, got %s", team.Summary.AttackTrend)
	}
}

func TestTrendTagAndHit(t *testing.T) {
	mk := func(totals ...int) trendTeam {
		list := make([]trendRecord, 0, len(totals))
		for _, total := range totals {
			list = append(list, trendRecord{Scored: total, Conceded: 0})
		}
		return trendTeam{Sample: len(list), List: list, Summary: trendSummarize(list)}
	}
	if _, tag, ok := trendTag(mk(3, 3, 3, 3, 3), mk(3, 3, 3, 3, 3), 5); tag != "大球倾向" || !ok {
		t.Fatalf("got %s", tag)
	}
	if _, tag, _ := trendTag(mk(1, 1, 1, 1, 1), mk(1, 2, 1, 2, 1), 5); tag != "小球倾向" {
		t.Fatalf("got %s", tag)
	}
	if _, tag, ok := trendTag(mk(3, 3, 3), mk(3, 3, 3, 3, 3), 5); tag != "样本不足" || ok {
		t.Fatalf("short sample should be 样本不足, got %s", tag)
	}
}
