package docker

import (
	"fmt"
	"time"
)

// HumanAge says how long before now t was, roughly, e.g. "just now",
// "5 minutes ago", "2 days ago", "3 weeks ago", "4 months ago" or
// "1 year ago". Days are counted up to 13, weeks up to 8 and months (of 30
// days) up to 12. A t after now, as clock skew between the host and the
// daemon can produce, counts as just now.
func HumanAge(t, now time.Time) string {
	d := now.Sub(t)
	days := int(d / (24 * time.Hour))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return ago(int(d/time.Minute), "minute")
	case d < 24*time.Hour:
		return ago(int(d/time.Hour), "hour")
	case days < 14:
		return ago(days, "day")
	case days < 60:
		return ago(days/7, "week")
	case days < 365:
		return ago(days/30, "month")
	default:
		return ago(days/365, "year")
	}
}

// ago formats n units ago, e.g. "1 day ago" or "3 weeks ago".
func ago(n int, unit string) string {
	if n == 1 {
		return "1 " + unit + " ago"
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}
