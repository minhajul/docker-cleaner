package docker

import (
	"testing"
)

func TestHumanSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1024 * 1024 * 1024 * 1024, "1.0 TB"},
	}
	for _, c := range cases {
		if got := HumanSize(c.in); got != c.want {
			t.Errorf("HumanSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatCleanupMessage(t *testing.T) {
	cases := []struct {
		name               string
		images, containers int
		want               string
	}{
		{"empty", 0, 0, ""},
		{"one image", 1, 0, "Successfully deleted 1 image"},
		{"many images", 5, 0, "Successfully deleted 5 images"},
		{"one container", 0, 1, "Successfully deleted 1 container"},
		{"many containers", 0, 3, "Successfully deleted 3 containers"},
		{"one of each", 1, 1, "Successfully deleted 1 image and 1 container"},
		{"mixed many", 3, 7, "Successfully deleted 3 images and 7 containers"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatCleanupMessage(c.images, c.containers); got != c.want {
				t.Errorf("formatCleanupMessage(%d, %d) = %q, want %q",
					c.images, c.containers, got, c.want)
			}
		})
	}
}
