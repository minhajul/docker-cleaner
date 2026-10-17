package docker_test

import (
	"strings"
	"testing"

	"github.com/minhajul/docker-cleaner/docker"
)

func TestShortVolumeName(t *testing.T) {
	anon := strings.Repeat("0123456789abcdef", 4)
	cases := map[string]string{
		anon:                  "0123456789ab",
		"pgdata":              "pgdata",
		strings.ToUpper(anon): strings.ToUpper(anon), // not the daemon's lowercase hex
		anon[:63]:             anon[:63],
	}
	for in, want := range cases {
		if got := docker.ShortVolumeName(in); got != want {
			t.Errorf("ShortVolumeName(%q) = %q, want %q", in, got, want)
		}
	}
}
