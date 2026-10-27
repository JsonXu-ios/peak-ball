package handlers

import (
	"strings"
	"testing"
)

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

func TestCrawlerSportteryRecordsSeparateUnopenedFromMissingAndFailure(t *testing.T) {
	for _, fixture := range []struct {
		body            string
		records, priced bool
	}{
		{`{"code":0,"data":{"spf":{"odds":[{"spf3":0,"spf1":0,"spf0":0}]}}}`, true, false},
		{`{"code":0,"data":{"spf":{"odds":[{"spf3":1.55,"spf1":3.85,"spf0":4.45}]}}}`, true, true},
		{`{"code":0,"data":{"spf":{"odds":[]}}}`, false, false},
		{`{"code":0,"data":{"spf":{"odds":[{"spf3":1.55,"spf1":3.85}]}}}`, false, false},
		{`{"code":1,"data":{"spf":{"odds":[{"spf3":0,"spf1":0,"spf0":0}]}}}`, false, false},
	} {
		if sportteryOddsJSONHasRecords([]byte(fixture.body)) != fixture.records || sportteryOddsJSONHasData([]byte(fixture.body)) != fixture.priced {
			t.Fatalf("wrong source state: %+v", fixture)
		}
	}
}

func TestCrawlerSportteryEmptySuccessfulResponseIsNotCollectionFailure(t *testing.T) {
	if SportteryOddsCaptureState([]byte(`{"code":0,"data":null}`)) != "no_quotes" || SportteryOddsCaptureState([]byte(`{"code":0}`)) != "invalid" {
		t.Fatal("absent data and a source's null market were confused")
	}
	if SportteryOddsCaptureState([]byte(`{"code":0,"data":{}}`)) != "no_quotes" || SportteryOddsCaptureState([]byte(`{"code":1,"data":{}}`)) != "invalid" {
		t.Fatal("source's empty market was confused with a failed request")
	}
}

func TestCrawlerSportteryRawNullIsNotDeletedByMergePatch(t *testing.T) {
	update := crawlerSportteryJSONUpdate(`{"sportteryOdds":{"code":0,"data":null}}`)
	if !strings.Contains(update.SQL, "JSON_SET(") || len(update.Vars) != 1 || update.Vars[0] != `{"code":0,"data":null}` {
		t.Fatal("raw source null would be removed instead of retained")
	}
	if !strings.Contains(crawlerSportteryJSONUpdate(`{"data":{"tzbl":{"h":1.36}}}`).SQL, "JSON_MERGE_PATCH(") {
		t.Fatal("trade-only refresh can remove the independent capture")
	}
}
