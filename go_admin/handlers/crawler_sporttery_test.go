package handlers

import "testing"

func TestCrawlerSportteryOddsRequiresSuccessfulSpfHistory(t *testing.T) {
	for _, fixture := range []struct {
		body  string
		valid bool
	}{
		{`{"code":0,"data":{"matchId":3073080,"spf":{"odds":[{"spf3":1.89,"spf1":3.55,"spf0":3.14}]}}}`, true},
		{`{"code":1,"data":{"spf":{"odds":[{"spf3":1.89,"spf1":3.55,"spf0":3.14}]}}}`, false},
		{`{"data":{"rqspf":{"odds":[{"rq3":4.2,"rq1":3.75,"rq0":1.6}]}}}`, false},
		{`{"data":{"spf":{"odds":[{"spf3":0,"spf1":0,"spf0":0}]}}}`, false},
		{`{"data":{"spf":{"odds":[{"spf3":1.89,"spf1":3.55}]}}}`, false},
		{`<html>403</html>`, false},
	} {
		if got := sportteryOddsJSONHasData([]byte(fixture.body)); got != fixture.valid {
			t.Fatalf("valid=%v expected=%v: %s", got, fixture.valid, fixture.body)
		}
	}
	if crawlerRequestType("https://www.vipc.cn/i/match/jczq/sporttery/498235450") != "sporttery_odds" {
		t.Fatal("new source lacks the collection rate limiter")
	}
}
