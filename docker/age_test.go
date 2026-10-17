package docker

import (
	"testing"
	"time"
)

func TestHumanAge(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{0, "just now"},
		{59 * time.Second, "just now"},
		{-time.Hour, "just now"}, // in the future: clock skew
		{time.Minute, "1 minute ago"},
		{59 * time.Minute, "59 minutes ago"},
		{time.Hour, "1 hour ago"},
		{23*time.Hour + 59*time.Minute, "23 hours ago"},
		{day, "1 day ago"},
		{2 * day, "2 days ago"},
		{13 * day, "13 days ago"},
		{14 * day, "2 weeks ago"},
		{21 * day, "3 weeks ago"},
		{59 * day, "8 weeks ago"},
		{60 * day, "2 months ago"},
		{364 * day, "12 months ago"},
		{365 * day, "1 year ago"},
		{3 * 365 * day, "3 years ago"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			if got := HumanAge(now.Add(-c.ago), now); got != c.want {
				t.Errorf("HumanAge(now - %v) = %q, want %q", c.ago, got, c.want)
			}
		})
	}
}
