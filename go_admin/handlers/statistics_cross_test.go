package handlers

import "testing"

// TestCrossScoreBucket keeps the buckets aligned with the 竞彩比分 purchasable
// options: 胜 covers up to 5:2, 平 up to 3:3, 负 up to 2:5, the rest fold into
// 胜其他/平其他/负其他.
func TestCrossScoreBucket(t *testing.T) {
	cases := []struct {
		home, guest int
		want        string
	}{
		{1, 0, "1:0"}, {2, 1, "2:1"}, {5, 2, "5:2"},
		{4, 3, "胜其他"}, {6, 0, "胜其他"},
		{0, 0, "0:0"}, {3, 3, "3:3"}, {4, 4, "平其他"},
		{0, 1, "0:1"}, {2, 5, "2:5"},
		{3, 4, "负其他"}, {0, 6, "负其他"},
	}
	for _, item := range cases {
		if got := crossScoreBucket(item.home, item.guest); got != item.want {
			t.Errorf("crossScoreBucket(%d,%d) = %s, want %s", item.home, item.guest, got, item.want)
		}
	}
}

func TestCrossGoalsBucket(t *testing.T) {
	cases := map[int]string{0: "0", 3: "3", 6: "6", 7: "7+", 11: "7+"}
	for total, want := range cases {
		if got := crossGoalsBucket(total); got != want {
			t.Errorf("crossGoalsBucket(%d) = %s, want %s", total, got, want)
		}
	}
}
